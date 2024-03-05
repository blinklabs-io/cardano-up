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
	p.config.Logger.Debug("initializing package manager")
	if err := p.state.Load(); err != nil {
		return fmt.Errorf("failed to load state: %s", err)
	}
	// TODO: replace this with syncing a repo and reading from disk
	p.availablePackages = append(p.availablePackages, RegistryPackages...)
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
	}
	tmpConfig := p.config
	if tmpConfig.Template == nil {
		tmpConfig.Template = NewTemplate(tmplVars)
	} else {
		tmpConfig.Template = tmpConfig.Template.WithVars(tmplVars)
	}
	p.config = tmpConfig
}

func (p *PackageManager) AvailablePackages() []Package {
	return p.availablePackages[:]
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
	var installedPkgs []string
	var notesOutput string
	for _, installPkg := range installPkgs {
		p.config.Logger.Info(
			fmt.Sprintf(
				"Installing package %s (= %s)",
				installPkg.Name,
				installPkg.Version,
			),
		)
		notes, err := installPkg.install(p.config, activeContextName)
		if err != nil {
			return err
		}
		installedPkg := NewInstalledPackage(installPkg, activeContextName, notes)
		p.state.InstalledPackages = append(p.state.InstalledPackages, installedPkg)
		if err := p.state.Save(); err != nil {
			return err
		}
		installedPkgs = append(installedPkgs, installPkg.Name)
		if notes != "" {
			notesOutput += fmt.Sprintf(
				"\nPost-install notes for %s (= %s):\n\n%s\n",
				installPkg.Name,
				installPkg.Version,
				notes,
			)
		}
	}
	// Display post-install notes
	if notesOutput != "" {
		p.config.Logger.Info(notesOutput)
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
	var installedPkgs []string
	var notesOutput string
	for _, upgradePkg := range upgradePkgs {
		p.config.Logger.Info(
			fmt.Sprintf(
				"Upgrading package %s (%s => %s)",
				upgradePkg.Installed.Package.Name,
				upgradePkg.Installed.Package.Version,
				upgradePkg.Upgrade.Version,
			),
		)
		// Uninstall old version
		if err := p.uninstallPackage(upgradePkg.Installed); err != nil {
			return err
		}
		// Install new version
		notes, err := upgradePkg.Upgrade.install(p.config, activeContextName)
		if err != nil {
			return err
		}
		installedPkg := NewInstalledPackage(upgradePkg.Upgrade, activeContextName, notes)
		p.state.InstalledPackages = append(p.state.InstalledPackages, installedPkg)
		if err := p.state.Save(); err != nil {
			return err
		}
		installedPkgs = append(installedPkgs, upgradePkg.Upgrade.Name)
		if notes != "" {
			notesOutput += fmt.Sprintf(
				"\nPost-install notes for %s (= %s):\n\n%s\n",
				upgradePkg.Upgrade.Name,
				upgradePkg.Upgrade.Version,
				notes,
			)
		}
		if err := p.state.Save(); err != nil {
			return err
		}
	}
	// Display post-install notes
	if notesOutput != "" {
		p.config.Logger.Info(notesOutput)
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

func (p *PackageManager) Uninstall(pkgs ...string) error {
	// Find installed packages
	activeContextName, _ := p.ActiveContext()
	installedPackages := p.InstalledPackages()
	var uninstallPkgs []InstalledPackage
	for _, pkg := range pkgs {
		foundPackage := false
		for _, tmpPackage := range installedPackages {
			if tmpPackage.Package.Name == pkg {
				foundPackage = true
				uninstallPkgs = append(
					uninstallPkgs,
					tmpPackage,
				)
				break
			}
		}
		if !foundPackage {
			return NewPackageNotInstalledError(pkg, activeContextName)
		}
	}
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
	for _, uninstallPkg := range uninstallPkgs {
		if err := p.uninstallPackage(uninstallPkg); err != nil {
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
	var infoOutput string
	for idx, infoPkg := range infoPkgs {
		infoOutput += fmt.Sprintf(
			"Name: %s\nVersion: %s\nContext: %s",
			infoPkg.Package.Name,
			infoPkg.Package.Version,
			activeContextName,
		)
		if infoPkg.PostInstallNotes != "" {
			infoOutput += fmt.Sprintf(
				"\n\nPost-install notes:\n\n%s",
				infoPkg.PostInstallNotes,
			)
		}
		// Gather package services
		services, err := infoPkg.Package.services(p.config, infoPkg.Context)
		if err != nil {
			return err
		}
		// Build service status and port output
		var statusOutput string
		var portOutput string
		for _, svc := range services {
			running, err := svc.Running()
			if err != nil {
				return err
			}
			if running {
				statusOutput += fmt.Sprintf(
					"%-60s RUNNING\n",
					svc.ContainerName,
				)
			} else {
				statusOutput += fmt.Sprintf(
					"%-60s NOT RUNNING\n",
					svc.ContainerName,
				)
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
				portOutput += fmt.Sprintf(
					"%-5s (host) => %-5s (container)\n",
					hostPort,
					containerPort,
				)
			}
		}
		if statusOutput != "" {
			infoOutput += fmt.Sprintf(
				"\n\nServices:\n\n%s",
				strings.TrimSuffix(statusOutput, "\n"),
			)
		}
		if portOutput != "" {
			infoOutput += fmt.Sprintf(
				"\n\nMapped ports:\n\n%s",
				strings.TrimSuffix(portOutput, "\n"),
			)
		}
		if idx < len(infoPkgs)-1 {
			infoOutput += "\n\n---\n\n"
		}
	}
	p.config.Logger.Info(infoOutput)
	return nil
}

func (p *PackageManager) uninstallPackage(uninstallPkg InstalledPackage) error {
	// Uninstall package
	if err := uninstallPkg.Package.uninstall(p.config, uninstallPkg.Context); err != nil {
		return err
	}
	// Remove package from installed packages
	var tmpInstalledPackages []InstalledPackage
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
	p.state.ActiveContext = name
	if err := p.state.Save(); err != nil {
		return err
	}
	// Update templating values
	p.initTemplate()
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
			tmpNetwork := ouroboros.NetworkByName(newContext.Network)
			if tmpNetwork == ouroboros.NetworkInvalid {
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
