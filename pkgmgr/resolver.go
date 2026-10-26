// Copyright 2024 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkgmgr

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/hashicorp/go-version"
)

type Resolver struct {
	context              string
	logger               *slog.Logger
	installedPkgs        []InstalledPackage
	availablePkgs        []Package
	installedConstraints map[string]version.Constraints
}

type ResolverInstallSet struct {
	Install  Package
	Options  map[string]bool
	Selected bool
}

type ResolverUpgradeSet struct {
	Installed InstalledPackage
	Upgrade   Package
	Options   map[string]bool
}

func NewResolver(
	installedPkgs []InstalledPackage,
	availablePkgs []Package,
	context string,
	logger *slog.Logger,
) (*Resolver, error) {
	r := &Resolver{
		context:              context,
		logger:               logger,
		installedPkgs:        installedPkgs[:],
		availablePkgs:        availablePkgs[:],
		installedConstraints: make(map[string]version.Constraints),
	}
	// Calculate package constraints from installed packages
	for _, installedPkg := range installedPkgs {
		for _, dependency := range installedPkg.Package.Dependencies {
			satisfied, err := r.satisfiedProviders(dependency, nil)
			if err != nil {
				return nil, err
			}
			for _, provider := range dependency {
				depPkgName, depPkgVersionSpec, _ := r.splitPackage(provider)
				if len(satisfied) > 1 ||
					(len(satisfied) == 1 && satisfied[0] != depPkgName) {
					continue
				}
				if depPkgVersionSpec == "" {
					continue
				}
				tmpConstraints, err := version.NewConstraint(depPkgVersionSpec)
				if err != nil {
					return nil, err
				}
				r.installedConstraints[depPkgName] = append(
					r.installedConstraints[depPkgName],
					tmpConstraints...,
				)
				logger.Debug(
					fmt.Sprintf(
						"added constraint for installed package %q dependency: %q: %s",
						installedPkg.Package.Name,
						depPkgName,
						tmpConstraints.String(),
					),
				)
			}
		}
	}
	return r, nil
}

func (r *Resolver) Install(pkgs ...string) ([]ResolverInstallSet, error) {
	ret := []ResolverInstallSet{}
	for _, pkg := range pkgs {
		pkgName, pkgVersionSpec, pkgOpts := r.splitPackage(pkg)
		if pkg, err := r.findInstalled(pkgName, ""); err != nil {
			return nil, err
		} else if !pkg.IsEmpty() {
			return nil, NewResolverPackageAlreadyInstalledError(pkgName)
		}
		latestPkg, err := r.latestAvailablePackage(pkgName, pkgVersionSpec, nil)
		if err != nil {
			return nil, err
		}
		if latestPkg.IsEmpty() {
			return nil, NewResolverNoAvailablePackage(pkg)
		}
		// Calculate dependencies
		neededPkgs, err := r.getNeededDeps(latestPkg, false)
		if err != nil {
			return nil, err
		}
		ret = append(ret, neededPkgs...)
		// Add selected package
		ret = append(
			ret,
			ResolverInstallSet{
				Install:  latestPkg,
				Selected: true,
				Options:  pkgOpts,
			},
		)
	}
	return ret, nil
}

func (r *Resolver) Upgrade(pkgs ...string) ([]ResolverUpgradeSet, error) {
	ret := []ResolverUpgradeSet{}
	for _, pkg := range pkgs {
		pkgName, pkgVersionSpec, pkgOpts := r.splitPackage(pkg)
		installedPkg, err := r.findInstalled(pkgName, "")
		if err != nil {
			return nil, err
		} else if installedPkg.IsEmpty() {
			return nil, NewPackageNotInstalledError(pkgName, r.context)
		}
		latestPkg, err := r.latestAvailablePackage(pkgName, pkgVersionSpec, nil)
		if err != nil {
			return nil, err
		}
		if latestPkg.Version == "" ||
			latestPkg.Version == installedPkg.Package.Version {
			return nil, NewNoPackageAvailableForUpgradeError(pkg)
		}
		ret = append(
			ret,
			ResolverUpgradeSet{
				Installed: installedPkg,
				Upgrade:   latestPkg,
				Options:   pkgOpts,
			},
		)
		// Calculate dependencies
		neededPkgs, err := r.getNeededDeps(latestPkg, true)
		if err != nil {
			return nil, err
		}
		for _, neededPkg := range neededPkgs {
			tmpInstalled, err := r.findInstalled(neededPkg.Install.Name, "")
			if err != nil {
				return nil, err
			}
			ret = append(
				ret,
				ResolverUpgradeSet{
					Installed: tmpInstalled,
					Upgrade:   neededPkg.Install,
					Options:   neededPkg.Options,
				},
			)
		}
	}
	return ret, nil
}

func (r *Resolver) Uninstall(pkgs ...InstalledPackage) error {
	excluded := make(map[string]struct{}, len(pkgs))
	for _, pkg := range pkgs {
		excluded[pkg.Package.Name] = struct{}{}
	}
	for _, pkg := range pkgs {
		for _, installedPkg := range r.installedPkgs {
			for _, dependency := range installedPkg.Package.Dependencies {
				matches, err := r.packageSatisfiesDependency(
					pkg.Package,
					dependency,
				)
				if err != nil {
					return err
				}
				if !matches {
					continue
				}
				satisfied, err := r.satisfiedProviders(
					dependency,
					excluded,
				)
				if err != nil {
					return err
				}
				if len(satisfied) > 0 {
					continue
				}
				return NewPackageUninstallWouldBreakDepsError(
					pkg.Package.Name,
					pkg.Package.Version,
					installedPkg.Package.Name,
					installedPkg.Package.Version,
				)
			}
		}
	}
	return nil
}

func (r *Resolver) getNeededDeps(
	pkg Package,
	allowUpgrades bool,
) ([]ResolverInstallSet, error) {
	// NOTE: this function is very naive and only works for a single level of dependencies
	ret := []ResolverInstallSet{}
	for _, dependency := range pkg.Dependencies {
		satisfied, err := r.satisfiedProviders(dependency, nil)
		if err != nil {
			return nil, err
		}
		if len(satisfied) > 0 {
			continue
		}
		var selected ResolverInstallSet
		for _, provider := range dependency {
			depPkgName, depPkgVersionSpec, depPkgOpts := r.splitPackage(
				provider,
			)
			installed, err := r.findInstalled(depPkgName, "")
			if err != nil {
				return nil, err
			}
			if !installed.IsEmpty() && !allowUpgrades {
				continue
			}
			availablePkgs, err := r.findAvailable(
				depPkgName,
				depPkgVersionSpec,
				nil,
			)
			if err != nil {
				return nil, err
			}
			if len(availablePkgs) == 0 {
				continue
			}
			latestPkg, err := r.latestPackage(availablePkgs, nil)
			if err != nil {
				return nil, err
			}
			selected = ResolverInstallSet{
				Install: latestPkg,
				Options: depPkgOpts,
			}
			break
		}
		if selected.Install.IsEmpty() {
			return nil, NewResolverNoAvailablePackageDependencyError(
				dependency.String(),
			)
		}
		ret = append(ret, selected)
	}
	return ret, nil
}

func (r *Resolver) satisfiedProviders(
	dependency PackageDependency,
	excluded map[string]struct{},
) ([]string, error) {
	var ret []string
	for _, provider := range dependency {
		pkgName, pkgVersionSpec, _ := r.splitPackage(provider)
		if _, ok := excluded[pkgName]; ok {
			continue
		}
		installed, err := r.findInstalled(pkgName, pkgVersionSpec)
		if err != nil {
			return nil, err
		}
		if !installed.IsEmpty() {
			ret = append(ret, pkgName)
		}
	}
	return ret, nil
}

func (r *Resolver) packageSatisfiesDependency(
	pkg Package,
	dependency PackageDependency,
) (bool, error) {
	pkgVersion, err := version.NewVersion(pkg.Version)
	if err != nil {
		return false, err
	}
	for _, provider := range dependency {
		pkgName, pkgVersionSpec, _ := r.splitPackage(provider)
		if pkg.Name != pkgName {
			continue
		}
		if pkgVersionSpec == "" {
			return true, nil
		}
		constraints, err := version.NewConstraint(pkgVersionSpec)
		if err != nil {
			return false, err
		}
		if constraints.Check(pkgVersion) {
			return true, nil
		}
	}
	return false, nil
}

func (r *Resolver) splitPackage(pkg string) (string, string, map[string]bool) {
	return splitPackage(pkg)
}

func splitPackage(pkg string) (string, string, map[string]bool) {
	var pkgName, pkgVersionSpec string
	pkgOpts := make(map[string]bool)
	// Extract any package option flags
	optsOpenIdx := strings.Index(pkg, `[`)
	optsCloseIdx := strings.Index(pkg, `]`)
	if optsOpenIdx > 0 && optsCloseIdx > optsOpenIdx {
		pkgName = pkg[:optsOpenIdx]
		tmpOpts := pkg[optsOpenIdx+1 : optsCloseIdx]
		tmpFlags := strings.SplitSeq(tmpOpts, `,`)
		for tmpFlag := range tmpFlags {
			flagVal := true
			if strings.HasPrefix(tmpFlag, `-`) {
				flagVal = false
				tmpFlag = tmpFlag[1:]
			}
			pkgOpts[tmpFlag] = flagVal
		}
	}
	// Extract version spec
	versionSpecIdx := strings.IndexAny(pkg, ` <>=~!`)
	if versionSpecIdx > 0 {
		if pkgName == "" {
			pkgName = pkg[:versionSpecIdx]
		}
		pkgVersionSpec = strings.TrimSpace(pkg[versionSpecIdx:])
	}
	// Use the original package name if we don't already have one from above
	if pkgName == "" {
		pkgName = pkg
	}
	return pkgName, pkgVersionSpec, pkgOpts
}

func (r *Resolver) findInstalled(
	pkgName string,
	pkgVersionSpec string,
) (InstalledPackage, error) {
	constraints := version.Constraints{}
	if pkgVersionSpec != "" {
		tmpConstraints, err := version.NewConstraint(pkgVersionSpec)
		if err != nil {
			return InstalledPackage{}, err
		}
		constraints = tmpConstraints
	}
	for _, installedPkg := range r.installedPkgs {
		if installedPkg.Package.Name != pkgName {
			continue
		}
		if pkgVersionSpec != "" {
			installedPkgVer, err := version.NewVersion(
				installedPkg.Package.Version,
			)
			if err != nil {
				return InstalledPackage{}, err
			}
			if !constraints.Check(installedPkgVer) {
				continue
			}
		}
		return installedPkg, nil
	}
	return InstalledPackage{}, nil
}

func (r *Resolver) findAvailable(
	pkgName string,
	pkgVersionSpec string,
	extraConstraints version.Constraints,
) ([]Package, error) {
	var constraints version.Constraints
	// Filter to versions matching our version spec
	if pkgVersionSpec != "" {
		tmpConstraints, err := version.NewConstraint(pkgVersionSpec)
		if err != nil {
			return nil, err
		}
		constraints = tmpConstraints
	}
	// Use installed package constraints if none provided
	if extraConstraints == nil {
		if r.installedConstraints != nil {
			if pkgConstraints, ok := r.installedConstraints[pkgName]; ok {
				extraConstraints = pkgConstraints
			}
		}
	}
	// Filter to versions matching provided constraints
	if extraConstraints != nil {
		constraints = append(
			constraints,
			extraConstraints...,
		)
	}
	ret := []Package{}
	for _, availablePkg := range r.availablePkgs {
		if availablePkg.Name != pkgName {
			continue
		}
		if constraints != nil {
			availablePkgVer, err := version.NewVersion(availablePkg.Version)
			if err != nil {
				return nil, err
			}
			if !constraints.Check(availablePkgVer) {
				r.logger.Debug(
					fmt.Sprintf(
						"excluding available package \"%s = %s\" due to constraint: %s",
						availablePkg.Name,
						availablePkg.Version,
						constraints.String(),
					),
				)
				continue
			}
		}
		ret = append(ret, availablePkg)
	}
	return ret, nil
}

func (r *Resolver) latestAvailablePackage(
	pkgName string,
	pkgVersionSpec string,
	constraints version.Constraints,
) (Package, error) {
	pkgs, err := r.findAvailable(pkgName, pkgVersionSpec, constraints)
	if err != nil {
		return Package{}, err
	}
	return r.latestPackage(pkgs, constraints)
}

func (r *Resolver) latestPackage(
	pkgs []Package,
	constraints version.Constraints,
) (Package, error) {
	var ret Package
	var retVer *version.Version
	for _, pkg := range pkgs {
		pkgVer, err := version.NewVersion(pkg.Version)
		if err != nil {
			return ret, err
		}
		// Skip package if it doesn't match provided constraints
		if len(constraints) > 0 {
			if !constraints.Check(pkgVer) {
				continue
			}
		}
		// Set this package as the latest if none set or newer than previous set
		if retVer == nil || pkgVer.GreaterThan(retVer) {
			ret = pkg
			retVer = pkgVer
		}
	}
	return ret, nil
}
