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
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	contextsFilename          = "contexts.yaml"
	activeContextFilename     = "active_context.yaml"
	installedPackagesFilename = "installed_packages.yaml"
	portRegistryFilename      = "port_registry.yaml"
)

type State struct {
	config            Config
	ActiveContext     string
	Contexts          map[string]Context
	InstalledPackages []InstalledPackage
	PortRegistry      PortRegistry
}

func NewState(cfg Config) *State {
	return &State{
		config:       cfg,
		Contexts:     make(map[string]Context),
		PortRegistry: make(PortRegistry),
	}
}

func (s *State) Load() error {
	if err := s.loadContexts(); err != nil {
		return err
	}
	if err := s.loadActiveContext(); err != nil {
		return err
	}
	if err := s.loadInstalledPackages(); err != nil {
		return err
	}
	if err := s.loadPortRegistry(); err != nil {
		return err
	}
	return nil
}

func (s *State) Save() error {
	if err := s.saveContexts(); err != nil {
		return err
	}
	if err := s.saveActiveContext(); err != nil {
		return err
	}
	if err := s.saveInstalledPackages(); err != nil {
		return err
	}
	if err := s.savePortRegistry(); err != nil {
		return err
	}
	return nil
}

func (s *State) loadFile(filename string, dest any) error {
	tmpPath := filepath.Join(
		s.config.ConfigDir,
		filename,
	)
	// Check if the file exists and we can access it
	if _, err := os.Stat(tmpPath); err != nil {
		// Treat no file like an empty file
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(content, dest); err != nil {
		return err
	}
	return nil
}

func (s *State) saveFile(filename string, src any) error {
	// Create parent directory if it doesn't exist
	if _, err := os.Stat(s.config.ConfigDir); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(s.config.ConfigDir, 0o700); err != nil {
				return err
			}
		}
	}
	tmpPath := filepath.Join(
		s.config.ConfigDir,
		filename,
	)
	yamlContent, err := yaml.Marshal(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, yamlContent, 0o600); err != nil {
		return err
	}
	return nil
}

func (s *State) loadContexts() error {
	if err := s.loadFile(contextsFilename, &(s.Contexts)); err != nil {
		return err
	}
	if len(s.Contexts) == 0 {
		s.Contexts[defaultContextName] = defaultContext
	}
	return nil
}

func (s *State) saveContexts() error {
	return s.saveFile(contextsFilename, &(s.Contexts))
}

func (s *State) loadActiveContext() error {
	if err := s.loadFile(activeContextFilename, &(s.ActiveContext)); err != nil {
		return err
	}
	if s.ActiveContext == "" {
		s.ActiveContext = defaultContextName
	}
	return nil
}

func (s *State) saveActiveContext() error {
	return s.saveFile(activeContextFilename, &(s.ActiveContext))
}

func (s *State) loadInstalledPackages() error {
	return s.loadFile(installedPackagesFilename, &(s.InstalledPackages))
}

func (s *State) saveInstalledPackages() error {
	return s.saveFile(installedPackagesFilename, &(s.InstalledPackages))
}

func (s *State) loadPortRegistry() error {
	if err := s.loadFile(portRegistryFilename, &(s.PortRegistry)); err != nil {
		return err
	}
	if s.PortRegistry == nil {
		s.PortRegistry = make(PortRegistry)
	}
	if len(s.PortRegistry) == 0 {
		if err := s.migrateLegacyPortRegistry(); err != nil {
			return err
		}
	}
	return nil
}

func (s *State) savePortRegistry() error {
	return s.saveFile(portRegistryFilename, &(s.PortRegistry))
}

func (s *State) migrateLegacyPortRegistry() error {
	contextsPath := filepath.Join(
		s.config.ConfigDir,
		contextsFilename,
	)
	content, err := os.ReadFile(contextsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	legacyContexts := make(map[string]struct {
		PortRegistry map[string]map[string]map[string]string `yaml:"portRegistry"`
	})
	if err := yaml.Unmarshal(content, &legacyContexts); err != nil {
		return err
	}
	migrated := false
	for contextName, legacyContext := range legacyContexts {
		if len(legacyContext.PortRegistry) == 0 {
			continue
		}
		if s.PortRegistry == nil {
			s.PortRegistry = make(PortRegistry)
		}
		contextRegistry := make(ContextPortRegistry, len(legacyContext.PortRegistry))
		for pkgName, pkgRegistry := range legacyContext.PortRegistry {
			contextRegistry[pkgName] = make(PackagePortRegistry, len(pkgRegistry))
			for svcName, svcPorts := range pkgRegistry {
				contextRegistry[pkgName][svcName] = cloneServicePortMap(ServicePortMap(svcPorts))
			}
		}
		s.PortRegistry[contextName] = contextRegistry
		migrated = true
	}
	if migrated {
		if err := s.savePortRegistry(); err != nil {
			return err
		}
	}
	return nil
}
