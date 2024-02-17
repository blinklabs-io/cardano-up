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
	// TODO: remove me
	if err := p.state.Save(); err != nil {
		return err
	}
	return nil
}

func (p *PackageManager) AvailablePackages() []Package {
	return p.availablePackages[:]
}

func (p *PackageManager) Install(pkg Package) error {
	return pkg.install(p.config)
}
