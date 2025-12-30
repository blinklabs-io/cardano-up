// Copyright 2025 Blink Labs Software
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
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	ouroboros "github.com/blinklabs-io/gouroboros"
)

type PackageManager struct {
	config            Config
	state             *State
	availablePackages []Package
}

func NewPackageManager(cfg Config) (*PackageManager, error) {
	// Make sure that a logger was provided, since we use it for pretty much all feedback
	if cfg.Logger == nil {
		return nil, errors.New("you must provide a logger")
	}
	p := &PackageManager{
		config: cfg,
		state:  NewState(cfg),
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return p, nil
}

func NewDefaultPackageManager() (*PackageManager, error) {
	pmCfg, err := NewDefaultConfig()
	if err != nil {
		return nil, err
	}
	return NewPackageManager(pmCfg)
}

func (p *PackageManager) init() error {
	if err := p.state.Load(); err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}
	// Setup templating
	p.initTemplate()
	return nil
}

func (p *PackageManager) initTemplate() {
	activeContextName, activeContext := p.ActiveContext()
	tmplVars := map[string]any{
		"Context": map[string]any{
			"Name":         activeContextName,
			"Network":      activeContext.Network,
			"NetworkMagic": activeContext.NetworkMagic,
		},
		"Env": p.ContextEnv(),
	}
	tmpConfig := p.config
	if tmpConfig.Template == nil {
		tmpConfig.Template = NewTemplate(tmplVars)
	} else {
		tmpConfig.Template = tmpConfig.Template.WithVars(tmplVars)
	}
	p.config = tmpConfig
}

func (p *PackageManager) loadPackageRegistry(validate bool) error {
	var retErr error
	registryPkgs, err := registryPackages(p.config, validate)
	if err != nil {
		if errors.Is(err, ErrValidationFailed) {
			// We want to pass along the validation error, but only after we record the packages
			retErr = err
		} else {
			return err
		}
	}
	p.availablePackages = registryPkgs[:]
	return retErr
}

func (p *PackageManager) AvailablePackages() []Package {
	var ret []Package
	if p.availablePackages == nil {
		if err := p.loadPackageRegistry(false); err != nil {
			p.config.Logger.Warn(
				fmt.Sprintf("failed to load packages: %s", err),
			)
		}
	}
	for _, pkg := range p.availablePackages {
		if pkg.hasTags(p.config.RequiredPackageTags) {
			ret = append(ret, pkg)
		}
	}
	return ret
}

func (p *PackageManager) Up() error {
	// Find installed packages
	installedPackages := p.InstalledPackages()
	for _, tmpPackage := range installedPackages {
		err := tmpPackage.Package.startService(p.config, tmpPackage.Context)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *PackageManager) Down() error {
	// Find installed packages
	installedPackages := p.InstalledPackages()
	for _, tmpPackage := range installedPackages {
		err := tmpPackage.Package.stopService(p.config, tmpPackage.Context)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *PackageManager) InstalledPackages() []InstalledPackage {
	var ret []InstalledPackage
	for _, pkg := range p.state.InstalledPackages {
		if pkg.Context == p.state.ActiveContext {
			ret = append(ret, pkg)
		}
	}
	return ret
}

func (p *PackageManager) InstalledPackagesAllContexts() []InstalledPackage {
	return p.state.InstalledPackages
}

func (p *PackageManager) Install(pkgs ...string) error {
	// Check context for network
	activeContextName, activeContext := p.ActiveContext()
	if activeContext.Network == "" {
		return ErrContextInstallNoNetwork
	}
	resolver, err := NewResolver(
		p.InstalledPackages(),
		p.AvailablePackages(),
		activeContextName,
		p.config.Logger,
	)
	if err != nil {
		return err
	}
	installPkgs, err := resolver.Install(pkgs...)
	if err != nil {
		return err
	}
	installedPkgs := []string{}
	var sb strings.Builder
	for _, installPkg := range installPkgs {
		p.config.Logger.Info(
			fmt.Sprintf(
				"Installing package %s (= %s)",
				installPkg.Install.Name,
				installPkg.Install.Version,
			),
		)
		// Build package options
		tmpPkgOpts := installPkg.Install.defaultOpts()
		maps.Copy(tmpPkgOpts, installPkg.Options)
		// Install package
		registeredPorts := p.registeredPorts(activeContextName, installPkg.Install.Name)
		notes, outputs, usedPorts, err := installPkg.Install.install(
			p.config,
			activeContextName,
			tmpPkgOpts,
			true,
			registeredPorts,
		)
		if err != nil {
			return err
		}
		installedPkg := NewInstalledPackage(
			installPkg.Install,
			activeContextName,
			notes,
			outputs,
			tmpPkgOpts,
		)
		p.state.InstalledPackages = append(
			p.state.InstalledPackages,
			installedPkg,
		)
		p.setRegisteredPorts(activeContextName, installPkg.Install.Name, usedPorts)
		if err := p.state.Save(); err != nil {
			return err
		}
		installedPkgs = append(installedPkgs, installPkg.Install.Name)
		if notes != "" {
			sb.WriteString("\nPost-install notes for ")
			sb.WriteString(installPkg.Install.Name)
			sb.WriteString(" (= ")
			sb.WriteString(installPkg.Install.Version)
			sb.WriteString("):\n\n")
			sb.WriteString(notes)
			sb.WriteString("\n")
		}
		// Activate package
		if err := installPkg.Install.activate(p.config, activeContextName); err != nil {
			p.config.Logger.Warn(
				fmt.Sprintf("failed to activate package: %s", err),
			)
		}
	}
	// Display post-install notes
	outStr := sb.String()
	if outStr != "" {
		p.config.Logger.Info(outStr)
	}
	p.config.Logger.Info(
		fmt.Sprintf(
			"Successfully installed package(s) in context %q: %s",
			activeContextName,
			strings.Join(installedPkgs, ", "),
		),
	)
	return nil
}

func (p *PackageManager) Upgrade(pkgs ...string) error {
	activeContextName, _ := p.ActiveContext()
	resolver, err := NewResolver(
		p.InstalledPackages(),
		p.AvailablePackages(),
		activeContextName,
		p.config.Logger,
	)
	if err != nil {
		return err
	}
	upgradePkgs, err := resolver.Upgrade(pkgs...)
	if err != nil {
		return err
	}
	installedPkgs := []string{}
	var sb strings.Builder
	for _, upgradePkg := range upgradePkgs {
		p.config.Logger.Info(
			fmt.Sprintf(
				"Upgrading package %s (%s => %s)",
				upgradePkg.Installed.Package.Name,
				upgradePkg.Installed.Package.Version,
				upgradePkg.Upgrade.Version,
			),
		)
		// Capture options from existing package
		pkgOpts := upgradePkg.Installed.Options
		registeredPorts := p.registeredPorts(activeContextName, upgradePkg.Installed.Package.Name)
		// Deactivate old package
		if err := upgradePkg.Installed.Package.deactivate(p.config, activeContextName); err != nil {
			p.config.Logger.Warn(
				fmt.Sprintf("failed to deactivate package: %s", err),
			)
		}
		// Uninstall old version
		if err := p.uninstallPackage(upgradePkg.Installed, true, false); err != nil {
			return err
		}
		// Install new version
		notes, outputs, usedPorts, err := upgradePkg.Upgrade.install(
			p.config,
			activeContextName,
			pkgOpts,
			false,
			registeredPorts,
		)
		if err != nil {
			return err
		}
		installedPkg := NewInstalledPackage(
			upgradePkg.Upgrade,
			activeContextName,
			notes,
			outputs,
			pkgOpts,
		)
		p.state.InstalledPackages = append(
			p.state.InstalledPackages,
			installedPkg,
		)
		p.setRegisteredPorts(activeContextName, upgradePkg.Upgrade.Name, usedPorts)
		if err := p.state.Save(); err != nil {
			return err
		}
		installedPkgs = append(installedPkgs, upgradePkg.Upgrade.Name)
		if notes != "" {
			sb.WriteString("\nPost-install notes for ")
			sb.WriteString(upgradePkg.Upgrade.Name)
			sb.WriteString(" (= ")
			sb.WriteString(upgradePkg.Upgrade.Version)
			sb.WriteString("):\n\n")
			sb.WriteString(notes)
			sb.WriteString("\n")
		}
		if err := p.state.Save(); err != nil {
			return err
		}
		// Activate new package
		if err := upgradePkg.Upgrade.activate(p.config, activeContextName); err != nil {
			p.config.Logger.Warn(
				fmt.Sprintf("failed to activate package: %s", err),
			)
		}
	}
	// Display post-install notes
	if sb.String() != "" {
		p.config.Logger.Info(sb.String())
	}
	p.config.Logger.Info(
		fmt.Sprintf(
			"Successfully upgraded/installed package(s) in context %q: %s",
			activeContextName,
			strings.Join(installedPkgs, ", "),
		),
	)
	return nil
}

func (p *PackageManager) Uninstall(
	pkgName string,
	keepData bool,
	force bool,
) error {
	// Find installed packages
	activeContextName, _ := p.ActiveContext()
	installedPackages := p.InstalledPackages()
	var uninstallPkgs []InstalledPackage
	foundPackage := false
	for _, tmpPackage := range installedPackages {
		if tmpPackage.Package.Name == pkgName {
			foundPackage = true
			uninstallPkgs = append(
				uninstallPkgs,
				tmpPackage,
			)
			break
		}
	}
	if !foundPackage {
		return NewPackageNotInstalledError(pkgName, activeContextName)
	}
	if !force {
		// Resolve dependencies
		resolver, err := NewResolver(
			p.InstalledPackages(),
			p.AvailablePackages(),
			activeContextName,
			p.config.Logger,
		)
		if err != nil {
			return err
		}
		if err := resolver.Uninstall(uninstallPkgs...); err != nil {
			return err
		}
	}
	for _, uninstallPkg := range uninstallPkgs {
		// Deactivate package
		if err := uninstallPkg.Package.deactivate(p.config, activeContextName); err != nil {
			p.config.Logger.Warn(
				fmt.Sprintf("failed to deactivate package: %s", err),
			)
		}
		if err := p.uninstallPackage(uninstallPkg, keepData, true); err != nil {
			return err
		}
		if err := p.state.Save(); err != nil {
			return err
		}
		p.config.Logger.Info(
			fmt.Sprintf(
				"Successfully uninstalled package %s (= %s) from context %q",
				uninstallPkg.Package.Name,
				uninstallPkg.Package.Version,
				activeContextName,
			),
		)
	}
	return nil
}

func (p *PackageManager) Logs(
	pkgName string,
	follow bool,
	tail string,
	stdoutWriter io.Writer,
	stderrWriter io.Writer,
) error {
	// Find installed packages
	activeContextName, _ := p.ActiveContext()
	installedPackages := p.InstalledPackages()
	var logsPkg InstalledPackage
	foundPackage := false
	for _, tmpPackage := range installedPackages {
		if tmpPackage.Package.Name == pkgName {
			foundPackage = true
			logsPkg = tmpPackage
			break
		}
	}
	if !foundPackage {
		return NewPackageNotInstalledError(pkgName, activeContextName)
	}
	services, err := logsPkg.Package.services(p.config, activeContextName)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return NewNoServicesFoundError(pkgName)
	}
	// TODO: account for more than one service in a package
	tmpSvc := services[0]
	if err := tmpSvc.Logs(follow, tail, stdoutWriter, stderrWriter); err != nil {
		return err
	}
	return nil
}

func (p *PackageManager) Info(pkgs ...string) error {
	// Find installed packages
	activeContextName, _ := p.ActiveContext()
	installedPackages := p.InstalledPackages()
	var infoPkgs []InstalledPackage
	for _, pkg := range pkgs {
		foundPackage := false
		for _, tmpPackage := range installedPackages {
			if tmpPackage.Package.Name == pkg {
				foundPackage = true
				infoPkgs = append(
					infoPkgs,
					tmpPackage,
				)
				break
			}
		}
		if !foundPackage {
			return NewPackageNotInstalledError(pkg, activeContextName)
		}
	}
	var sb strings.Builder
	for idx, infoPkg := range infoPkgs {
		sb.WriteString("Name: ")
		sb.WriteString(infoPkg.Package.Name)
		sb.WriteString("\nVersion: ")
		sb.WriteString(infoPkg.Package.Version)
		sb.WriteString("\nContext: ")
		sb.WriteString(activeContextName)
		if infoPkg.PostInstallNotes != "" {
			sb.WriteString(
				"\n\nPost-install notes:\n\n" + infoPkg.PostInstallNotes,
			)
		}
		// Gather package services
		services, err := infoPkg.Package.services(p.config, infoPkg.Context)
		if err != nil {
			return err
		}
		// Build service status and port output
		var statusSb strings.Builder
		var portSb strings.Builder
		for _, svc := range services {
			running, err := svc.Running()
			if err != nil {
				return err
			}
			if running {
				statusSb.WriteString(fmt.Sprintf(
					"%-60s RUNNING\n",
					svc.ContainerName,
				))
			} else {
				statusSb.WriteString(fmt.Sprintf(
					"%-60s NOT RUNNING\n",
					svc.ContainerName,
				))
			}
			for _, port := range svc.Ports {
				var containerPort, hostPort string
				portParts := strings.Split(port, ":")
				switch len(portParts) {
				case 1:
					containerPort = portParts[0]
					hostPort = portParts[0]
				case 2:
					containerPort = portParts[1]
					hostPort = portParts[0]
				case 3:
					containerPort = portParts[2]
					hostPort = portParts[1]
				}
				portSb.WriteString(fmt.Sprintf(
					"%-5s (host) => %-5s (container)\n",
					hostPort,
					containerPort,
				))
			}
		}
		if statusSb.String() != "" {
			sb.WriteString("\n\nServices:\n\n" + strings.TrimSuffix(
				statusSb.String(),
				"\n",
			))
		}
		if portSb.String() != "" {
			sb.WriteString("\n\nMapped ports:\n\n" + strings.TrimSuffix(
				portSb.String(),
				"\n",
			))
		}
		if idx < len(infoPkgs)-1 {
			sb.WriteString("\n\n---\n\n")
		}
	}
	p.config.Logger.Info(sb.String())
	return nil
}

func (p *PackageManager) uninstallPackage(
	uninstallPkg InstalledPackage,
	keepData bool,
	runHooks bool,
) error {
	// Uninstall package
	if err := uninstallPkg.Package.uninstall(p.config, uninstallPkg.Context, keepData, runHooks); err != nil {
		return err
	}
	// Remove package from installed packages
	tmpInstalledPackages := []InstalledPackage{}
	for _, tmpInstalledPkg := range p.state.InstalledPackages {
		if tmpInstalledPkg.Context == uninstallPkg.Context &&
			tmpInstalledPkg.Package.Name == uninstallPkg.Package.Name &&
			tmpInstalledPkg.Package.Version == uninstallPkg.Package.Version {
			continue
		}
		tmpInstalledPackages = append(tmpInstalledPackages, tmpInstalledPkg)
	}
	p.state.InstalledPackages = tmpInstalledPackages[:]
	return nil
}

func (p *PackageManager) Contexts() map[string]Context {
	return p.state.Contexts
}

func (p *PackageManager) ActiveContext() (string, Context) {
	return p.state.ActiveContext, p.state.Contexts[p.state.ActiveContext]
}

func (p *PackageManager) AddContext(name string, context Context) error {
	if _, ok := p.state.Contexts[name]; ok {
		return ErrContextAlreadyExists
	}
	// Create dummy context entry
	p.state.Contexts[name] = Context{}
	// Update dummy context
	if err := p.updateContext(name, context); err != nil {
		return err
	}
	return nil
}

func (p *PackageManager) DeleteContext(name string) error {
	if name == p.state.ActiveContext {
		return ErrContextNoDeleteActive
	}
	if _, ok := p.state.Contexts[name]; !ok {
		return ErrContextNotExist
	}
	delete(p.state.Contexts, name)
	if err := p.state.Save(); err != nil {
		return err
	}
	return nil
}

func (p *PackageManager) SetActiveContext(name string) error {
	if _, ok := p.state.Contexts[name]; !ok {
		return ErrContextNotExist
	}
	// Deactivate packages in current context
	activeContextName, _ := p.ActiveContext()
	for _, pkg := range p.InstalledPackages() {
		if err := pkg.Package.deactivate(p.config, activeContextName); err != nil {
			p.config.Logger.Warn(
				fmt.Sprintf("failed to deactivate package: %s", err),
			)
		}
	}
	p.state.ActiveContext = name
	if err := p.state.Save(); err != nil {
		return err
	}
	// Update templating values
	p.initTemplate()
	// Activate packages in new context
	for _, pkg := range p.InstalledPackages() {
		if err := pkg.Package.activate(p.config, name); err != nil {
			p.config.Logger.Warn(
				fmt.Sprintf("failed to activate package: %s", err),
			)
		}
	}
	return nil
}

func (p *PackageManager) UpdateContext(name string, context Context) error {
	if err := p.updateContext(name, context); err != nil {
		return err
	}
	return nil
}

func (p *PackageManager) updateContext(name string, newContext Context) error {
	// Get current state of named context
	curContext, ok := p.state.Contexts[name]
	if !ok {
		return ErrContextNotExist
	}
	if curContext.Network != "" {
		// Check that we're not changing the network once configured
		if newContext.Network != curContext.Network {
			return ErrContextNoChangeNetwork
		}
	} else {
		// Check network name if setting it for new/empty context
		if newContext.Network != "" {
			tmpNetwork, ok := ouroboros.NetworkByName(newContext.Network)
			if !ok {
				return NewUnknownNetworkError(newContext.Network)
			}
			newContext.NetworkMagic = tmpNetwork.NetworkMagic
		}
	}
	p.state.Contexts[name] = newContext
	if err := p.state.Save(); err != nil {
		return err
	}
	// Update templating values
	p.initTemplate()
	return nil
}

func (p *PackageManager) ContextEnv() map[string]string {
	ret := make(map[string]string)
	for _, pkg := range p.InstalledPackages() {
		maps.Copy(ret, pkg.Outputs)
	}
	return ret
}

func (p *PackageManager) UpdatePackages() error {
	// Clear out existing cache files
	cachePath := filepath.Join(
		p.config.CacheDir,
		"registry",
	)
	if err := os.RemoveAll(cachePath); err != nil {
		return err
	}
	// (Re)load the package registry
	if err := p.loadPackageRegistry(false); err != nil {
		return err
	}
	return nil
}

func (p *PackageManager) ValidatePackages() error {
	foundError := false
	if len(p.availablePackages) == 0 {
		if err := p.loadPackageRegistry(true); err != nil {
			if errors.Is(err, ErrValidationFailed) {
				// Record error for later failure
				// The error(s) will have already been output to the console
				foundError = true
			} else {
				return err
			}
		}
	}
	for _, pkg := range p.availablePackages {
		if pkg.filePath == "" {
			continue
		}
		p.config.Logger.Debug(
			"checking package " + pkg.filePath,
		)
		if err := pkg.validate(p.config); err != nil {
			foundError = true
			p.config.Logger.Warn(
				fmt.Sprintf(
					"validation failed: %s: %s",
					pkg.filePath,
					err.Error(),
				),
			)
		}
	}
	if foundError {
		return ErrOperationFailed
	}
	return nil
}

func (p *PackageManager) registeredPorts(
	contextName string,
	pkgName string,
) PackagePortRegistry {
	context, ok := p.state.Contexts[contextName]
	if !ok {
		return nil
	}
	if len(context.PortRegistry) == 0 {
		return nil
	}
	if ports, ok := context.PortRegistry[pkgName]; ok {
		return clonePackagePortRegistry(ports)
	}
	return nil
}

func (p *PackageManager) setRegisteredPorts(
	contextName string,
	pkgName string,
	ports PackagePortRegistry,
) {
	context := p.state.Contexts[contextName]
	if context.PortRegistry == nil {
		context.PortRegistry = make(ContextPortRegistry)
	}
	if len(ports) == 0 {
		delete(context.PortRegistry, pkgName)
	} else {
		context.PortRegistry[pkgName] = clonePackagePortRegistry(ports)
	}
	p.state.Contexts[contextName] = context
}

// clonePackagePortRegistry returns a copy of the provided package port registry.
func clonePackagePortRegistry(src PackagePortRegistry) PackagePortRegistry {
	if len(src) == 0 {
		return nil
	}
	dst := make(PackagePortRegistry, len(src))
	for svc, ports := range src {
		dstMap := make(ServicePortMap, len(ports))
		if len(ports) == 0 {
			dst[svc] = nil
		}
		for k, v := range ports {
			dstMap[k] = v
		}
		dst[svc] = dstMap
	}
	return dst
}
