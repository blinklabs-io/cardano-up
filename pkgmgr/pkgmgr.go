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

type PackageManager struct {
	config Config
	// TODO
}

func NewPackageManager(cfg Config) (*PackageManager, error) {
	p := &PackageManager{
		config: cfg,
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
	// TODO: create config/cache dirs
	p.config.Logger.Debug("initializing package manager")
	return nil
}
