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
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Package struct {
	Name             string               `yaml:"name"`
	Version          string               `yaml:"version"`
	Description      string               `yaml:"description"`
	InstallSteps     []PackageInstallStep `yaml:"installSteps"`
	Dependencies     []string             `yaml:"dependencies"`
	Tags             []string             `yaml:"tags"`
	PostInstallNotes string               `yaml:"postInstallNotes"`
}

func (p Package) hasTags(tags []string) bool {
	for _, tag := range tags {
		foundTag := false
		for _, pkgTag := range p.Tags {
			if tag == pkgTag {
				foundTag = true
				break
			}
		}
		if !foundTag {
			return false
		}
	}
	return true
}

func (p Package) install(cfg Config, context string) (string, error) {
	// Update template vars
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	pkgCacheDir := filepath.Join(
		cfg.CacheDir,
		pkgName,
	)
	pkgDataDir := filepath.Join(
		cfg.DataDir,
		pkgName,
	)
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"Package": map[string]any{
				"Name":      pkgName,
				"ShortName": p.Name,
				"Version":   p.Version,
			},
			"Paths": map[string]string{
				"CacheDir": pkgCacheDir,
				"DataDir":  pkgDataDir,
			},
		},
	)
	// Run pre-flight checks
	for _, installStep := range p.InstallSteps {
		// Make sure only one install method is specified per install step
		if installStep.Docker != nil &&
			installStep.File != nil {
			return "", ErrMultipleInstallMethods
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.preflight(cfg, pkgName); err != nil {
				return "", fmt.Errorf("pre-flight check failed: %s", err)
			}
		}
	}
	// Pre-create dirs
	if err := os.MkdirAll(pkgCacheDir, fs.ModePerm); err != nil {
		return "", err
	}
	if err := os.MkdirAll(pkgDataDir, fs.ModePerm); err != nil {
		return "", err
	}
	// Perform install
	for _, installStep := range p.InstallSteps {
		if installStep.Docker != nil {
			if err := installStep.Docker.install(cfg, pkgName); err != nil {
				return "", err
			}
		} else if installStep.File != nil {
			if err := installStep.File.install(cfg, pkgName); err != nil {
				return "", err
			}
		} else {
			return "", ErrNoInstallMethods
		}
	}
	// Render notes and return
	if p.PostInstallNotes != "" {
		tmpNotes, err := cfg.Template.Render(p.PostInstallNotes, nil)
		if err != nil {
			return "", err
		}
		return tmpNotes, nil
	}
	return "", nil
}

func (p Package) uninstall(cfg Config, context string, keepData bool) error {
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
			if err := installStep.Docker.uninstall(cfg, pkgName, keepData); err != nil {
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
	if keepData {
		cfg.Logger.Debug(
			"skipping cleanup of package data/cache directories",
		)
	} else {
		// Remove package cache dir
		// We use Docker to deal with root-owned files from containers
		pkgCacheDir := filepath.Join(
			cfg.CacheDir,
			pkgName,
		)
		_, _, err := RunCommandInDocker(
			dockerUtilityImage,
			[]string{
				"rm",
				"-rf",
				fmt.Sprintf("%s/%s", cfg.CacheDir, pkgName),
			},
			[]string{
				fmt.Sprintf("%s:%s", cfg.CacheDir, cfg.CacheDir),
			},
		)
		if err != nil {
			cfg.Logger.Warn(
				fmt.Sprintf(
					"failed to remove package cache directory %q: %s",
					pkgCacheDir,
					err,
				),
			)
		} else {
			cfg.Logger.Debug(
				fmt.Sprintf(
					"removed package cache directory %q",
					pkgCacheDir,
				),
			)
		}
		// Remove package data dir
		// We use Docker to deal with root-owned files from containers
		pkgDataDir := filepath.Join(
			cfg.DataDir,
			pkgName,
		)
		_, _, err = RunCommandInDocker(
			dockerUtilityImage,
			[]string{
				"rm",
				"-rf",
				fmt.Sprintf("%s/%s", cfg.DataDir, pkgName),
			},
			[]string{
				fmt.Sprintf("%s:%s", cfg.DataDir, cfg.DataDir),
			},
		)
		if err != nil {
			cfg.Logger.Warn(
				fmt.Sprintf(
					"failed to remove package data directory %q: %s",
					pkgDataDir,
					err,
				),
			)
		} else {
			cfg.Logger.Debug(
				fmt.Sprintf(
					"removed package data directory %q",
					pkgDataDir,
				),
			)
		}
	}
	return nil
}

func (p Package) startService(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)

	var startErrors []string
	for _, step := range p.InstallSteps {
		if step.Docker != nil {
			containerName := fmt.Sprintf("%s-%s", pkgName, step.Docker.ContainerName)
			dockerService, err := NewDockerServiceFromContainerName(containerName, cfg.Logger)
			if err != nil {
				startErrors = append(startErrors, fmt.Sprintf("error initializing Docker service for container %s: %v", containerName, err))
				continue
			}
			// Start the Docker container if it's not running
			slog.Info(fmt.Sprintf("Starting Docker container %s", containerName))
			if err := dockerService.Start(); err != nil {
				startErrors = append(startErrors, fmt.Sprintf("failed to start Docker container %s: %v", containerName, err))
			}
		}
	}

	if len(startErrors) > 0 {
		slog.Error(strings.Join(startErrors, "\n"))
		return ErrOperationFailed
	}

	return nil
}

func (p Package) stopService(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)

	var stopErrors []string
	for _, step := range p.InstallSteps {
		if step.Docker != nil {
			containerName := fmt.Sprintf("%s-%s", pkgName, step.Docker.ContainerName)
			dockerService, err := NewDockerServiceFromContainerName(containerName, cfg.Logger)
			if err != nil {
				stopErrors = append(stopErrors, fmt.Sprintf("error initializing Docker service for container %s: %v", containerName, err))
				continue
			}
			// Stop the Docker container
			slog.Info(fmt.Sprintf("Stopping container %s", containerName))
			if err := dockerService.Stop(); err != nil {
				stopErrors = append(stopErrors, fmt.Sprintf("failed to stop Docker container %s: %v", containerName, err))
			}
		}
	}

	if len(stopErrors) > 0 {
		slog.Error(strings.Join(stopErrors, "\n"))
		return ErrOperationFailed
	}

	return nil
}

func (p Package) services(cfg Config, context string) ([]*DockerService, error) {
	var ret []*DockerService
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	for _, step := range p.InstallSteps {
		if step.Docker != nil {
			containerName := fmt.Sprintf("%s-%s", pkgName, step.Docker.ContainerName)
			dockerService, err := NewDockerServiceFromContainerName(containerName, cfg.Logger)
			if err != nil {
				cfg.Logger.Error(
					fmt.Sprintf("error initializing Docker service for container %s: %v", containerName, err),
				)
				continue
			}
			ret = append(ret, dockerService)
		}
	}
	return ret, nil
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
	PullOnly      bool              `yaml:"pullOnly"`
}

func (p *PackageInstallStepDocker) preflight(cfg Config, pkgName string) error {
	if err := CheckDockerConnectivity(); err != nil {
		return err
	}
	containerName := fmt.Sprintf("%s-%s", pkgName, p.ContainerName)
	if _, err := NewDockerServiceFromContainerName(containerName, cfg.Logger); err != nil {
		if err == ErrContainerNotExists {
			// Container does not exist (we want this)
			return nil
		} else {
			return err
		}
	} else {
		return ErrContainerAlreadyExists
	}
	return nil
}

func (p *PackageInstallStepDocker) install(cfg Config, pkgName string) error {
	containerName := fmt.Sprintf("%s-%s", pkgName, p.ContainerName)
	extraVars := map[string]any{
		"Container": map[string]any{
			"Name": containerName,
		},
	}
	tmpImage, err := cfg.Template.Render(p.Image, extraVars)
	if err != nil {
		return err
	}
	tmpEnv := make(map[string]string)
	for k, v := range p.Env {
		tmplVal, err := cfg.Template.Render(v, extraVars)
		if err != nil {
			return err
		}
		tmpEnv[k] = tmplVal
	}
	var tmpCommand []string
	for _, cmd := range p.Command {
		tmpCmd, err := cfg.Template.Render(cmd, extraVars)
		if err != nil {
			return err
		}
		tmpCommand = append(tmpCommand, tmpCmd)
	}
	var tmpArgs []string
	for _, arg := range p.Args {
		tmpArg, err := cfg.Template.Render(arg, extraVars)
		if err != nil {
			return err
		}
		tmpArgs = append(tmpArgs, tmpArg)
	}
	var tmpBinds []string
	for _, bind := range p.Binds {
		tmpBind, err := cfg.Template.Render(bind, extraVars)
		if err != nil {
			return err
		}
		tmpBinds = append(tmpBinds, tmpBind)
	}
	var tmpPorts []string
	for _, port := range p.Ports {
		tmpPort, err := cfg.Template.Render(port, extraVars)
		if err != nil {
			return err
		}
		tmpPorts = append(tmpPorts, tmpPort)
	}
	svc := DockerService{
		logger:        cfg.Logger,
		ContainerName: containerName,
		Image:         tmpImage,
		Env:           tmpEnv,
		Command:       tmpCommand,
		Args:          tmpArgs,
		Binds:         tmpBinds,
		Ports:         tmpPorts,
	}
	if p.PullOnly {
		if err := svc.pullImage(); err != nil {
			return err
		}
	} else {
		if err := svc.Create(); err != nil {
			return err
		}
		if err := svc.Start(); err != nil {
			return err
		}
	}
	return nil
}

func (p *PackageInstallStepDocker) uninstall(cfg Config, pkgName string, keepData bool) error {
	if !p.PullOnly {
		containerName := fmt.Sprintf("%s-%s", pkgName, p.ContainerName)
		svc, err := NewDockerServiceFromContainerName(containerName, cfg.Logger)
		if err != nil {
			if err == ErrContainerNotExists {
				cfg.Logger.Debug(
					fmt.Sprintf(
						"container missing on uninstall: %s",
						containerName,
					),
				)
			} else {
				return err
			}
		} else {
			if running, _ := svc.Running(); running {
				if err := svc.Stop(); err != nil {
					return err
				}
			}
			if err := svc.Remove(); err != nil {
				return err
			}
		}
	}
	if keepData {
		cfg.Logger.Debug(
			fmt.Sprintf(
				"skipping deletion of docker image %q",
				p.Image,
			),
		)
	} else {
		if err := RemoveDockerImage(p.Image); err != nil {
			cfg.Logger.Debug(
				fmt.Sprintf(
					"failed to delete image %q: %s",
					p.Image,
					err,
				),
			)
		} else {
			cfg.Logger.Debug(
				fmt.Sprintf(
					"removed unused image %q",
					p.Image,
				),
			)
		}
	}
	return nil
}

type PackageInstallStepFile struct {
	Binary   bool        `yaml:"binary"`
	Filename string      `yaml:"filename"`
	Content  string      `yaml:"content"`
	Mode     fs.FileMode `yaml:"mode,omitempty"`
}

func (p *PackageInstallStepFile) install(cfg Config, pkgName string) error {
	tmpFilePath, err := cfg.Template.Render(p.Filename, nil)
	if err != nil {
		return err
	}
	filePath := filepath.Join(
		cfg.DataDir,
		pkgName,
		tmpFilePath,
	)
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, fs.ModePerm); err != nil {
		return err
	}
	fileMode := fs.ModePerm
	if p.Mode > 0 {
		fileMode = p.Mode
	}
	tmpContent, err := cfg.Template.Render(p.Content, nil)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filePath, []byte(tmpContent), fileMode); err != nil {
		return err
	}
	cfg.Logger.Debug(fmt.Sprintf("wrote file %s", filePath))
	if p.Binary {
		binPath := filepath.Join(
			cfg.BinDir,
			tmpFilePath,
		)
		parentDir := filepath.Dir(binPath)
		if err := os.MkdirAll(parentDir, fs.ModePerm); err != nil {
			return err
		}
		if err := os.Symlink(filePath, binPath); err != nil {
			return err
		}
		cfg.Logger.Debug(fmt.Sprintf("wrote symlink from %s to %s", binPath, filePath))
	}
	return nil
}

func (p *PackageInstallStepFile) uninstall(cfg Config, pkgName string) error {
	filePath := filepath.Join(
		cfg.DataDir,
		pkgName,
		p.Filename,
	)
	cfg.Logger.Debug(fmt.Sprintf("deleting file %s", filePath))
	if err := os.Remove(filePath); err != nil {
		cfg.Logger.Warn(fmt.Sprintf("failed to remove file %s", filePath))
	}
	if p.Binary {
		binPath := filepath.Join(
			cfg.BinDir,
			p.Filename,
		)
		if err := os.Remove(binPath); err != nil {
			cfg.Logger.Warn(fmt.Sprintf("failed to remove symlink %s", binPath))
		}
	}
	return nil
}
