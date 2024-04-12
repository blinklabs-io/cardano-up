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
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/creack/pty"
	"github.com/hashicorp/go-version"
	"gopkg.in/yaml.v3"
)

type Package struct {
	Name                string               `yaml:"name,omitempty"`
	Version             string               `yaml:"version,omitempty"`
	Description         string               `yaml:"description,omitempty"`
	InstallSteps        []PackageInstallStep `yaml:"installSteps,omitempty"`
	Dependencies        []string             `yaml:"dependencies,omitempty"`
	Tags                []string             `yaml:"tags,omitempty"`
	PreInstallScript    string               `yaml:"preInstallScript,omitempty"`
	PostInstallScript   string               `yaml:"postInstallScript,omitempty"`
	PreUninstallScript  string               `yaml:"preUninstallScript,omitempty"`
	PostUninstallScript string               `yaml:"postUninstallScript,omitempty"`
	PostInstallNotes    string               `yaml:"postInstallNotes,omitempty"`
	Options             []PackageOption      `yaml:"options,omitempty"`
	Outputs             []PackageOutput      `yaml:"outputs,omitempty"`
	filePath            string
}

type PackageOption struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Default     bool   `yaml:"default"`
}

type PackageOutput struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Value       string `yaml:"value"`
}

func NewPackageFromFile(path string) (Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return Package{}, err
	}
	defer f.Close()
	return NewPackageFromReader(f)
}

func NewPackageFromReader(r io.Reader) (Package, error) {
	var ret Package
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&ret); err != nil {
		return Package{}, err
	}
	return ret, nil
}

func (p Package) IsEmpty() bool {
	return p.Name == "" && p.Version == ""
}

func (p Package) defaultOpts() map[string]bool {
	ret := make(map[string]bool)
	for _, opt := range p.Options {
		ret[opt.Name] = opt.Default
	}
	return ret
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

func (p Package) install(cfg Config, context string, opts map[string]bool, runHooks bool) (string, map[string]string, error) {
	// Update template vars
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	pkgCacheDir := filepath.Join(
		cfg.CacheDir,
		pkgName,
	)
	pkgContextDir := filepath.Join(
		cfg.DataDir,
		context,
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
				"Options":   opts,
			},
			"Paths": map[string]string{
				"CacheDir":   pkgCacheDir,
				"ContextDir": pkgContextDir,
				"DataDir":    pkgDataDir,
			},
		},
	)
	// Run pre-flight checks
	for _, installStep := range p.InstallSteps {
		// Make sure only one install method is specified per install step
		if installStep.Docker != nil &&
			installStep.File != nil {
			return "", nil, ErrMultipleInstallMethods
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.preflight(cfg, pkgName); err != nil {
				return "", nil, fmt.Errorf("pre-flight check failed: %s", err)
			}
		}
	}
	// Pre-create dirs
	if err := os.MkdirAll(pkgCacheDir, fs.ModePerm); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(pkgContextDir, fs.ModePerm); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(pkgDataDir, fs.ModePerm); err != nil {
		return "", nil, err
	}
	// Run pre-install script
	if runHooks && p.PreInstallScript != "" {
		if err := p.runHookScript(cfg, p.PreInstallScript); err != nil {
			return "", nil, err
		}
	}
	// Perform install
	for _, installStep := range p.InstallSteps {
		// Evaluate condition if defined
		if installStep.Condition != "" {
			if ok, err := cfg.Template.EvaluateCondition(installStep.Condition, nil); err != nil {
				return "", nil, NewInstallStepConditionError(installStep.Condition, err)
			} else if !ok {
				cfg.Logger.Debug(
					fmt.Sprintf(
						"skipping install step due to condition: %s",
						installStep.Condition,
					),
				)
				continue
			}
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.install(cfg, pkgName); err != nil {
				return "", nil, err
			}
		} else if installStep.File != nil {
			if err := installStep.File.install(cfg, pkgName, p.filePath); err != nil {
				return "", nil, err
			}
		} else {
			return "", nil, ErrNoInstallMethods
		}
	}
	// Capture port details for output templates
	tmpPorts := map[string]map[string]string{}
	tmpServices, err := p.services(cfg, context)
	if err != nil {
		return "", nil, err
	}
	for _, svc := range tmpServices {
		shortContainerName := strings.TrimPrefix(svc.ContainerName, pkgName+`-`)
		tmpPortsContainer := make(map[string]string)
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
			tmpPortsContainer[containerPort] = hostPort
		}
		tmpPorts[shortContainerName] = tmpPortsContainer
	}
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"Ports": tmpPorts,
		},
	)
	// Generate outputs
	retOutputs := make(map[string]string)
	for _, output := range p.Outputs {
		// Create key from package name and output name
		key := fmt.Sprintf(
			"%s_%s",
			p.Name,
			output.Name,
		)
		// Replace all characters that won't work in an env var
		envRe := regexp.MustCompile(`[^A-Za-z0-9_]+`)
		key = string(envRe.ReplaceAll([]byte(key), []byte(`_`)))
		// Make uppercase
		key = strings.ToUpper(key)
		// Render value template
		val, err := cfg.Template.Render(output.Value, nil)
		if err != nil {
			return "", nil, err
		}
		retOutputs[key] = val
	}
	// Run post-install script
	if runHooks && p.PostInstallScript != "" {
		if err := p.runHookScript(cfg, p.PostInstallScript); err != nil {
			return "", nil, err
		}
	}
	// Render notes and return
	var retNotes string
	if p.PostInstallNotes != "" {
		tmpNotes, err := cfg.Template.Render(p.PostInstallNotes, nil)
		if err != nil {
			return "", nil, err
		}
		retNotes = tmpNotes
	}
	return retNotes, retOutputs, nil
}

func (p Package) uninstall(cfg Config, context string, keepData bool, runHooks bool) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	// Run pre-uninstall script
	if runHooks && p.PreUninstallScript != "" {
		if err := p.runHookScript(cfg, p.PreUninstallScript); err != nil {
			return err
		}
	}
	// Iterate over install steps in reverse
	for idx := len(p.InstallSteps) - 1; idx >= 0; idx-- {
		installStep := p.InstallSteps[idx]
		// Evaluate condition if defined
		if installStep.Condition != "" {
			if ok, err := cfg.Template.EvaluateCondition(installStep.Condition, nil); err != nil {
				return NewInstallStepConditionError(installStep.Condition, err)
			} else if !ok {
				cfg.Logger.Debug(
					fmt.Sprintf(
						"skipping uninstall step due to condition: %s",
						installStep.Condition,
					),
				)
				continue
			}
		}
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
		pkgCacheDir := filepath.Join(
			cfg.CacheDir,
			pkgName,
		)
		if err := os.RemoveAll(pkgCacheDir); err != nil {
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
		pkgDataDir := filepath.Join(
			cfg.DataDir,
			pkgName,
		)
		if err := os.RemoveAll(pkgDataDir); err != nil {
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
	// Run post-uninstall script
	if runHooks && p.PostUninstallScript != "" {
		if err := p.runHookScript(cfg, p.PostUninstallScript); err != nil {
			return err
		}
	}
	return nil
}

func (p Package) activate(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	for _, installStep := range p.InstallSteps {
		// Evaluate condition if defined
		if installStep.Condition != "" {
			if ok, err := cfg.Template.EvaluateCondition(installStep.Condition, nil); err != nil {
				return NewInstallStepConditionError(installStep.Condition, err)
			} else if !ok {
				cfg.Logger.Debug(
					fmt.Sprintf(
						"skipping install step due to condition: %s",
						installStep.Condition,
					),
				)
				continue
			}
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.activate(cfg, pkgName); err != nil {
				return err
			}
		} else if installStep.File != nil {
			if err := installStep.File.activate(cfg, pkgName); err != nil {
				return err
			}
		} else {
			return ErrNoInstallMethods
		}
	}
	return nil
}

func (p Package) deactivate(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	for _, installStep := range p.InstallSteps {
		// Evaluate condition if defined
		if installStep.Condition != "" {
			if ok, err := cfg.Template.EvaluateCondition(installStep.Condition, nil); err != nil {
				return NewInstallStepConditionError(installStep.Condition, err)
			} else if !ok {
				cfg.Logger.Debug(
					fmt.Sprintf(
						"skipping install step due to condition: %s",
						installStep.Condition,
					),
				)
				continue
			}
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.deactivate(cfg, pkgName); err != nil {
				return err
			}
		} else if installStep.File != nil {
			if err := installStep.File.deactivate(cfg, pkgName); err != nil {
				return err
			}
		} else {
			return ErrNoInstallMethods
		}
	}
	return nil
}

func (p Package) validate(cfg Config) error {
	// Check empty name
	if p.Name == "" {
		return fmt.Errorf("package name cannot be empty")
	}
	// Check name matches allowed characters
	reName := regexp.MustCompile(`^[-a-zA-Z0-9]+$`)
	if !reName.Match([]byte(p.Name)) {
		return fmt.Errorf("invalid package name: %s", p.Name)
	}
	// Check empty version
	if p.Version == "" {
		return fmt.Errorf("package version cannot be empty")
	}
	// Check version is well formed
	if _, err := version.NewVersion(p.Version); err != nil {
		return fmt.Errorf("package version is malformed: %s", err)
	}
	// Check if package path matches package name/version
	expectedFilePath := filepath.Join(
		p.Name,
		fmt.Sprintf(
			"%s-%s.yaml",
			p.Name,
			p.Version,
		),
	)
	if !strings.HasSuffix(p.filePath, expectedFilePath) {
		return fmt.Errorf("package did not have expected file path: %s", expectedFilePath)
	}
	// Validate install steps
	for _, installStep := range p.InstallSteps {
		// Evaluate condition if defined
		if installStep.Condition != "" {
			if _, err := cfg.Template.EvaluateCondition(installStep.Condition, nil); err != nil {
				return NewInstallStepConditionError(installStep.Condition, err)
			}
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.validate(cfg); err != nil {
				return err
			}
		} else if installStep.File != nil {
			if err := installStep.File.validate(cfg); err != nil {
				return err
			}
		} else {
			return ErrNoInstallMethods
		}
	}
	return nil
}

func (p Package) startService(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)

	var startErrors []string
	for _, step := range p.InstallSteps {
		if step.Docker != nil {
			if step.Docker.PullOnly {
				continue
			}
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
			if step.Docker.PullOnly {
				continue
			}
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
			if step.Docker.PullOnly {
				continue
			}
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

func (p Package) runHookScript(cfg Config, hookScript string) error {
	renderedScript, err := cfg.Template.Render(hookScript, nil)
	if err != nil {
		return fmt.Errorf("failed to render hook script template: %s", err)
	}
	cmd := exec.Command("/bin/sh", "-c", renderedScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// We won't be reading or writing, so throw away the PTY file
	_, err = pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to run hook script: %s", err)
	}
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("run hook script exited with error: %s", err)
	}
	return nil
}

type PackageInstallStep struct {
	Condition string                    `yaml:"condition,omitempty"`
	Docker    *PackageInstallStepDocker `yaml:"docker,omitempty"`
	File      *PackageInstallStepFile   `yaml:"file,omitempty"`
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

func (p *PackageInstallStepDocker) validate(cfg Config) error {
	if p.Image == "" {
		return fmt.Errorf("docker image must be provided")
	}
	// TODO: add more checks
	return nil
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
	}
	return ErrContainerAlreadyExists
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
		// Precreate any host paths for container bind mounts. This is necessary to retain non-root ownership
		bindParts := strings.SplitN(tmpBind, ":", 2)
		if bindParts != nil {
			hostPath := bindParts[0]
			if err := os.MkdirAll(hostPath, fs.ModePerm); err != nil {
				return err
			}
			cfg.Logger.Debug(
				fmt.Sprintf(
					"precreating host path for container bind mount: %q",
					hostPath,
				),
			)
		}
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

func (p *PackageInstallStepDocker) activate(cfg Config, pkgName string) error {
	// Nothing to do
	return nil
}

func (p *PackageInstallStepDocker) deactivate(cfg Config, pkgName string) error {
	// Nothing to do
	return nil
}

type PackageInstallStepFile struct {
	Binary   bool        `yaml:"binary"`
	Filename string      `yaml:"filename"`
	Source   string      `yaml:"source"`
	Content  string      `yaml:"content"`
	Mode     fs.FileMode `yaml:"mode,omitempty"`
}

func (p *PackageInstallStepFile) validate(cfg Config) error {
	// TODO: add checks
	return nil
}

func (p *PackageInstallStepFile) install(cfg Config, pkgName string, packagePath string) error {
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
	fileContent := p.Content
	if p.Source != "" {
		fullSourcePath := filepath.Join(
			filepath.Dir(packagePath),
			p.Source,
		)
		tmpContent, err := os.ReadFile(fullSourcePath)
		if err != nil {
			return err
		}
		fileContent = string(tmpContent)
	}
	fileContent, err = cfg.Template.Render(fileContent, nil)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filePath, []byte(fileContent), fileMode); err != nil {
		return err
	}
	cfg.Logger.Debug(fmt.Sprintf("wrote file %s", filePath))
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
		if !errors.Is(err, fs.ErrNotExist) {
			cfg.Logger.Warn(fmt.Sprintf("failed to remove file %s", filePath))
		}
	}
	return nil
}

func (p *PackageInstallStepFile) activate(cfg Config, pkgName string) error {
	if p.Binary {
		tmpFilePath, err := cfg.Template.Render(p.Filename, nil)
		if err != nil {
			return err
		}
		filePath := filepath.Join(
			cfg.DataDir,
			pkgName,
			p.Filename,
		)
		binPath := filepath.Join(
			cfg.BinDir,
			tmpFilePath,
		)
		parentDir := filepath.Dir(binPath)
		if err := os.MkdirAll(parentDir, fs.ModePerm); err != nil {
			return err
		}
		// Check for existing file at symlink location
		if stat, err := os.Lstat(binPath); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		} else {
			if (stat.Mode() & fs.ModeSymlink) > 0 {
				// Remove existing symlink
				if err := os.Remove(binPath); err != nil {
					if !errors.Is(err, fs.ErrNotExist) {
						return err
					}
				}
				cfg.Logger.Debug(
					fmt.Sprintf("removed existing symlink %q", binPath),
				)
			} else {
				return fmt.Errorf("will not overwrite existing file %q with symlink", binPath)
			}
		}
		if err := os.Symlink(filePath, binPath); err != nil {
			return err
		}
		cfg.Logger.Debug(fmt.Sprintf("wrote symlink from %s to %s", binPath, filePath))
	}
	return nil
}

func (p *PackageInstallStepFile) deactivate(cfg Config, pkgName string) error {
	if p.Binary {
		tmpFilePath, err := cfg.Template.Render(p.Filename, nil)
		if err != nil {
			return err
		}
		binPath := filepath.Join(
			cfg.BinDir,
			tmpFilePath,
		)
		parentDir := filepath.Dir(binPath)
		if err := os.MkdirAll(parentDir, fs.ModePerm); err != nil {
			return err
		}
		if err := os.Remove(binPath); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		cfg.Logger.Debug(fmt.Sprintf("removed symlink %s", binPath))
	}
	return nil
}
