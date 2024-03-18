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
	"errors"
	"fmt"
)

// ErrOperationFailed is a placeholder error for operations that directly log errors.
// It's used to signify when an operation has failed when the actual error message is
// sent through the provided logger
var ErrOperationFailed = errors.New("the operation has failed")

// ErrMultipleInstallMethods is returned when a package's install steps specify more than one install method
// on a single install step
var ErrMultipleInstallMethods = errors.New("only one install method may be specified in an install step")

// ErrNoInstallMethods is returned when a package's install steps include an install step which has no
// recognized install method specified
var ErrNoInstallMethods = errors.New("no supported install method specified on install step")

// ErrContextNotExist is returned when trying to selecting/managing a context that does not exist
var ErrContextNotExist = errors.New("context does not exist")

// ErrContextAlreadyExists is returned when creating a context with a name that is already in use
var ErrContextAlreadyExists = errors.New("specified context already exists")

// ErrContextNoChangeNetwork is returned when updating a context with a network different than what was previously configured
var ErrContextNoChangeNetwork = errors.New("cannot change the configured network for a context")

// ErrContextInstallNoNetwork is returned when performing an install with no network specified on the active context
var ErrContextInstallNoNetwork = errors.New("no network specified for context")

// ErrContextNoDeleteActive is returned when attempting to delete the active context
var ErrContextNoDeleteActive = errors.New("cannot delete active context")

// ErrContainerAlreadyExists is returned when creating a new container with a name that is already in use
var ErrContainerAlreadyExists = errors.New("specified container already exists")

// ErrContainerNotExists is returned when querying a container by name that doesn't exist
var ErrContainerNotExists = errors.New("specified container does not exist")

// ErrNoRegistryConfigured is returned when no registry is configured
var ErrNoRegistryConfigured = errors.New("no package registry is configured")

// ErrValidationFailed is returned when loading the package registry while doing package validation when a package failed to load
var ErrValidationFailed = errors.New("validation failed")

func NewUnknownNetworkError(networkName string) error {
	return fmt.Errorf(
		"unknown network %q",
		networkName,
	)
}

func NewResolverPackageAlreadyInstalledError(pkgName string) error {
	return fmt.Errorf(
		"the package %q is already installed in the current context\n\nYou can use 'cardano-up context create' to create an empty context to install another instance of the package",
		pkgName,
	)
}

func NewResolverNoAvailablePackageDependencyError(depSpec string) error {
	return fmt.Errorf(
		"no available package found for dependency: %s",
		depSpec,
	)
}

func NewResolverNoAvailablePackage(pkgSpec string) error {
	return fmt.Errorf(
		"no available package found: %s",
		pkgSpec,
	)
}

func NewResolverInstalledPackageNoMatchVersionSpecError(pkgName string, pkgVersion string, depSpec string) error {
	return fmt.Errorf(
		"installed package \"%s = %s\" does not match dependency: %s",
		pkgName,
		pkgVersion,
		depSpec,
	)
}

func NewPackageNotInstalledError(pkgName string, context string) error {
	return fmt.Errorf(
		"package %q is not installed in context %q",
		pkgName,
		context,
	)
}

func NewPackageUninstallWouldBreakDepsError(uninstallPkgName string, uninstallPkgVersion string, dependentPkgName string, dependentPkgVersion string) error {
	return fmt.Errorf(
		`uninstall of package "%s = %s" would break dependencies for package "%s = %s"`,
		uninstallPkgName,
		uninstallPkgVersion,
		dependentPkgName,
		dependentPkgVersion,
	)
}

func NewNoPackageAvailableForUpgradeError(pkgSpec string) error {
	return fmt.Errorf(
		"no package available for upgrade: %s",
		pkgSpec,
	)
}

func NewInstallStepConditionError(condition string, err error) error {
	return fmt.Errorf(
		"failure evaluating install step condition %q: %s",
		condition,
		err,
	)
}

func NewNoServicesFoundError(pkgName string) error {
	return fmt.Errorf(
		"no services found for package %q",
		pkgName,
	)
}
