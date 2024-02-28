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

	ouroboros "github.com/blinklabs-io/gouroboros"
)

type PackageManager struct {
	config            Config
	state             *State
	availablePackages []Package
	// TODO
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
		p.config.Logger,
	)
	if err != nil {
		return err
	}
	installPkgs, err := resolver.Install(pkgs...)
	if err != nil {
		return err
	}
	for _, installPkg := range installPkgs {
		if err := installPkg.install(p.config, activeContextName); err != nil {
			return err
		}
		installedPkg := NewInstalledPackage(installPkg, activeContextName)
		p.state.InstalledPackages = append(p.state.InstalledPackages, installedPkg)
		if err := p.state.Save(); err != nil {
			return err
		}
		p.config.Logger.Info(
			fmt.Sprintf(
				"Successfully installed package %s (= %s) in context %q",
				installPkg.Name,
				installPkg.Version,
				activeContextName,
			),
		)
	}
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
		p.config.Logger,
	)
	if err != nil {
		return err
	}
	if err := resolver.Uninstall(uninstallPkgs...); err != nil {
		return err
	}
	for _, uninstallPkg := range uninstallPkgs {
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
