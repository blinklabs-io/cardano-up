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
	Name         string
	Version      string
	Description  string
	InstallSteps []PackageInstallStep
}

func (p Package) install(cfg Config) error {
	pkgName := fmt.Sprintf("%s-%s", p.Name, p.Version)
	for _, installStep := range p.InstallSteps {
		// Make sure only one install method is specified per install step
		if installStep.Docker != nil &&
			installStep.File != nil {
			return ErrMultipleInstallMethods
		}
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

type PackageInstallStep struct {
	Docker *PackageInstallStepDocker
	File   *PackageInstallStepFile
}

type PackageInstallStepDocker struct {
	ContainerName string
	Image         string
	Env           map[string]string
	Command       []string
	Args          []string
	Binds         []string
	Ports         []string
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

type PackageInstallStepFile struct {
	Filename string
	Content  string
	Template bool
	Mode     fs.FileMode
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
