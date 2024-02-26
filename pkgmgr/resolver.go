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
	logger               *slog.Logger
	installedPkgs        []InstalledPackage
	availablePkgs        []Package
	installedConstraints map[string]version.Constraints
}

func NewResolver(installedPkgs []InstalledPackage, availablePkgs []Package, logger *slog.Logger) (*Resolver, error) {
	r := &Resolver{
		logger:               logger,
		installedPkgs:        installedPkgs[:],
		availablePkgs:        availablePkgs[:],
		installedConstraints: make(map[string]version.Constraints),
	}
	// Calculate package constraints from installed packages
	for _, installedPkg := range installedPkgs {
		pkgName := installedPkg.Package.Name
		// Add implicit constraint for other versions of the same package
		tmpConstraints, err := version.NewConstraint(
			fmt.Sprintf("= %s", installedPkg.Package.Version),
		)
		if err != nil {
			return nil, err
		}
		if _, ok := r.installedConstraints[pkgName]; !ok {
			r.installedConstraints[pkgName] = make(version.Constraints, 0)
		}
		r.installedConstraints[pkgName] = append(
			r.installedConstraints[pkgName],
			tmpConstraints...,
		)
		logger.Debug(
			fmt.Sprintf(
				"added constraint for installed package %q: %s",
				pkgName,
				tmpConstraints.String(),
			),
		)
		// Add constraint for each explicit dependency
		for _, dep := range installedPkg.Package.Dependencies {
			depPkgName, depPkgVersionSpec := r.splitPackage(dep)
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
					pkgName,
					depPkgName,
					tmpConstraints.String(),
				),
			)
		}
	}
	return r, nil
}

func (r *Resolver) Install(pkgs ...string) ([]Package, error) {
	var ret []Package
	for _, pkg := range pkgs {
		pkgName, pkgVersion := r.splitPackage(pkg)
		if pkg, err := r.findInstalled(pkgName, ""); err != nil {
			return nil, err
		} else if !pkg.InstalledTime.IsZero() {
			return nil, NewResolverPackageAlreadyInstalledError(pkgName)
		}
		availablePkgs, err := r.findAvailable(pkgName, pkgVersion)
		if err != nil {
			return nil, err
		}
		latestPkg, err := r.latestPackage(availablePkgs...)
		if err != nil {
			return nil, err
		}
		// Calculate dependencies
		neededPkgs, err := r.getNeededDeps(latestPkg)
		if err != nil {
			return nil, err
		}
		ret = append(ret, neededPkgs...)
		// Add selected package
		ret = append(ret, latestPkg)
	}
	return ret, nil
}

func (r *Resolver) getNeededDeps(pkg Package) ([]Package, error) {
	// NOTE: this function is very naive and only works for a single level of dependencies
	var ret []Package
	for _, dep := range pkg.Dependencies {
		depPkgName, depPkgVersionSpec := r.splitPackage(dep)
		// Check if we already have an installed package that satisfies the dependency
		if pkg, err := r.findInstalled(depPkgName, depPkgVersionSpec); err != nil {
			return nil, err
		} else if !pkg.InstalledTime.IsZero() {
			continue
		}
		// Check if we already have any installed version of the package
		if pkg, err := r.findInstalled(depPkgName, depPkgVersionSpec); err != nil {
			return nil, err
		} else if !pkg.InstalledTime.IsZero() {
			return nil, NewResolverInstalledPackageNoMatchVersionSpecError(pkg.Package.Name, pkg.Package.Version, dep)
		}
		availablePkgs, err := r.findAvailable(depPkgName, depPkgVersionSpec)
		if err != nil {
			return nil, err
		}
		if len(availablePkgs) == 0 {
			return nil, NewResolverNoAvailablePackageDependencyError(dep)
		}
		latestPkg, err := r.latestPackage(availablePkgs...)
		if err != nil {
			return nil, err
		}
		ret = append(ret, latestPkg)
	}
	return ret, nil
}

func (r *Resolver) splitPackage(pkg string) (string, string) {
	versionSpecIdx := strings.IndexAny(pkg, ` <>=~!`)
	var pkgName, pkgVersionSpec string
	if versionSpecIdx == -1 {
		pkgName = pkg
	} else {
		pkgName = pkg[:versionSpecIdx]
		pkgVersionSpec = strings.TrimSpace(pkg[versionSpecIdx:])
	}
	return pkgName, pkgVersionSpec
}

func (r *Resolver) findInstalled(pkgName string, pkgVersionSpec string) (InstalledPackage, error) {
	var constraints version.Constraints
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
			installedPkgVer, err := version.NewVersion(installedPkg.Package.Version)
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

func (r *Resolver) findAvailable(pkgName string, pkgVersionSpec string) ([]Package, error) {
	var constraints version.Constraints
	// Filter to versions matching our version spec
	if pkgVersionSpec != "" {
		tmpConstraints, err := version.NewConstraint(pkgVersionSpec)
		if err != nil {
			return nil, err
		}
		constraints = tmpConstraints
	}
	// Filter to versions matching constraints from installed packages
	if r.installedConstraints != nil {
		if pkgConstraints, ok := r.installedConstraints[pkgName]; ok {
			constraints = append(
				constraints,
				pkgConstraints...,
			)
		}
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

func (r *Resolver) latestPackage(pkgs ...Package) (Package, error) {
	var ret Package
	var retVer *version.Version
	for _, pkg := range pkgs {
		pkgVer, err := version.NewVersion(pkg.Version)
		if err != nil {
			return ret, err
		}
		if retVer == nil || pkgVer.GreaterThan(retVer) {
			ret = pkg
			retVer = pkgVer
		}
	}
	return ret, nil
}
