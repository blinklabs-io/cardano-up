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
	"io/fs"
	"os"
	"path/filepath"
)

type Package struct {
	Name         string               `yaml:"name"`
	Version      string               `yaml:"version"`
	Description  string               `yaml:"description"`
	InstallSteps []PackageInstallStep `yaml:"installSteps"`
}

func (p Package) install(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	// Run pre-flight checks
	for _, installStep := range p.InstallSteps {
		// Make sure only one install method is specified per install step
		if installStep.Docker != nil &&
			installStep.File != nil {
			return ErrMultipleInstallMethods
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.preflight(cfg, pkgName); err != nil {
				return fmt.Errorf("pre-flight check failed: %s", err)
			}
		}
	}
	// Perform install
	for _, installStep := range p.InstallSteps {
		if installStep.Docker != nil {
			if err := installStep.Docker.install(cfg, pkgName); err != nil {
				return err
			}
		} else if installStep.File != nil {
			if err := installStep.File.install(cfg, pkgName); err != nil {
				return err
			}
		} else {
			return ErrNoInstallMethods
		}
	}
	return nil
}

func (p Package) uninstall(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	// Iterate over install steps in reverse
	for idx := len(p.InstallSteps) - 1; idx >= 0; idx-- {
		installStep := p.InstallSteps[idx]
		// Make sure only one install method is specified per install step
		if installStep.Docker != nil &&
			installStep.File != nil {
			return ErrMultipleInstallMethods
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.uninstall(cfg, pkgName); err != nil {
				return err
			}
		} else if installStep.File != nil {
			if err := installStep.File.uninstall(cfg, pkgName); err != nil {
				return err
			}
		} else {
			return ErrNoInstallMethods
		}
	}
	return nil
}

type PackageInstallStep struct {
	Docker *PackageInstallStepDocker `yaml:"docker,omitempty"`
	File   *PackageInstallStepFile   `yaml:"file,omitempty"`
}

type PackageInstallStepDocker struct {
	ContainerName string            `yaml:"containerName"`
	Image         string            `yaml:"image,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	Command       []string          `yaml:"command,omitempty"`
	Args          []string          `yaml:"args,omitempty"`
	Binds         []string          `yaml:"binds,omitempty"`
	Ports         []string          `yaml:"ports,omitempty"`
}

func (p *PackageInstallStepDocker) preflight(cfg Config, pkgName string) error {
	if err := CheckDockerConnectivity(); err != nil {
		return err
	}
	containerName := fmt.Sprintf("%s-%s", pkgName, p.ContainerName)
	svc, err := NewDockerServiceFromContainerName(containerName, cfg.Logger)
	if err != nil {
		return err
	}
	if svc != nil {
		return ErrContainerAlreadyExists
	}
	return nil
}

func (p *PackageInstallStepDocker) install(cfg Config, pkgName string) error {
	containerName := fmt.Sprintf("%s-%s", pkgName, p.ContainerName)
	svc := DockerService{
		logger:        cfg.Logger,
		ContainerName: containerName,
		Image:         p.Image,
		Env:           p.Env,
		Command:       p.Command,
		Args:          p.Args,
		Binds:         p.Binds,
		Ports:         p.Ports,
	}
	if err := svc.Create(); err != nil {
		return err
	}
	if err := svc.Start(); err != nil {
		return err
	}
	return nil
}

func (p *PackageInstallStepDocker) uninstall(cfg Config, pkgName string) error {
	containerName := fmt.Sprintf("%s-%s", pkgName, p.ContainerName)
	svc, err := NewDockerServiceFromContainerName(containerName, cfg.Logger)
	if err != nil {
		return err
	}
	if running, _ := svc.Running(); running {
		if err := svc.Stop(); err != nil {
			return err
		}
	}
	if err := svc.Remove(); err != nil {
		return err
	}
	return nil
}

type PackageInstallStepFile struct {
	Filename string      `yaml:"filename"`
	Content  string      `yaml:"content"`
	Template bool        `yaml:"template"`
	Mode     fs.FileMode `yaml:"mode,omitempty"`
}

func (p *PackageInstallStepFile) install(cfg Config, pkgName string) error {
	// TODO: add templating support
	filePath := filepath.Join(
		cfg.ConfigDir,
		"data",
		pkgName,
		p.Filename,
	)
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, fs.ModePerm); err != nil {
		return err
	}
	fileMode := fs.ModePerm
	if p.Mode > 0 {
		fileMode = p.Mode
	}
	if err := os.WriteFile(filePath, []byte(p.Content), fileMode); err != nil {
		return err
	}
	cfg.Logger.Debug(fmt.Sprintf("wrote file %s", filePath))
	return nil
}

func (p *PackageInstallStepFile) uninstall(cfg Config, pkgName string) error {
	filePath := filepath.Join(
		cfg.ConfigDir,
		"data",
		pkgName,
		p.Filename,
	)
	cfg.Logger.Debug(fmt.Sprintf("deleting file %s", filePath))
	return os.Remove(filePath)
}
