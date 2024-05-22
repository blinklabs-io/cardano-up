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
		// Add constraint for each explicit dependency
		for _, dep := range installedPkg.Package.Dependencies {
			depPkgName, depPkgVersionSpec, _ := r.splitPackage(dep)
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
	return r, nil
}

func (r *Resolver) Install(pkgs ...string) ([]ResolverInstallSet, error) {
	var ret []ResolverInstallSet
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
		neededPkgs, err := r.getNeededDeps(latestPkg)
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
	var ret []ResolverUpgradeSet
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
		neededPkgs, err := r.getNeededDeps(latestPkg)
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
	for _, pkg := range pkgs {
		pkgVersion, err := version.NewVersion(pkg.Package.Version)
		if err != nil {
			return err
		}
		for _, installedPkg := range r.installedPkgs {
			for _, dep := range installedPkg.Package.Dependencies {
				depPkgName, depPkgVersionSpec, _ := r.splitPackage(dep)
				// Skip installed package if it doesn't match dep package name
				if pkg.Package.Name != depPkgName {
					continue
				}
				// Skip installed packages that don't match the specified dep version constraint
				if depPkgVersionSpec != "" {
					constraints, err := version.NewConstraint(depPkgVersionSpec)
					if err != nil {
						return err
					}
					if !constraints.Check(pkgVersion) {
						continue
					}
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

func (r *Resolver) getNeededDeps(pkg Package) ([]ResolverInstallSet, error) {
	// NOTE: this function is very naive and only works for a single level of dependencies
	var ret []ResolverInstallSet
	for _, dep := range pkg.Dependencies {
		depPkgName, depPkgVersionSpec, depPkgOpts := r.splitPackage(dep)
		// Check if we already have an installed package that satisfies the dependency
		if pkg, err := r.findInstalled(depPkgName, depPkgVersionSpec); err != nil {
			return nil, err
		} else if !pkg.IsEmpty() {
			continue
		}
		// Check if we already have any installed version of the package
		if pkg, err := r.findInstalled(depPkgName, depPkgVersionSpec); err != nil {
			return nil, err
		} else if !pkg.IsEmpty() {
			return nil, NewResolverInstalledPackageNoMatchVersionSpecError(pkg.Package.Name, pkg.Package.Version, dep)
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
			return nil, NewResolverNoAvailablePackageDependencyError(dep)
		}
		latestPkg, err := r.latestPackage(availablePkgs, nil)
		if err != nil {
			return nil, err
		}
		ret = append(
			ret,
			ResolverInstallSet{
				Install: latestPkg,
				Options: depPkgOpts,
			},
		)
	}
	return ret, nil
}

func (r *Resolver) splitPackage(pkg string) (string, string, map[string]bool) {
	var pkgName, pkgVersionSpec string
	pkgOpts := make(map[string]bool)
	// Extract any package option flags
	optsOpenIdx := strings.Index(pkg, `[`)
	optsCloseIdx := strings.Index(pkg, `]`)
	if optsOpenIdx > 0 && optsCloseIdx > optsOpenIdx {
		pkgName = pkg[:optsOpenIdx]
		tmpOpts := pkg[optsOpenIdx+1 : optsCloseIdx]
		tmpFlags := strings.Split(tmpOpts, `,`)
		for _, tmpFlag := range tmpFlags {
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
	var ret []Package
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
