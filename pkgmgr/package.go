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
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/docker/go-connections/nat"
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
	PreStartScript      string               `yaml:"preStartScript,omitempty"`
	PostStartScript     string               `yaml:"postStartScript,omitempty"`
	PreStopScript       string               `yaml:"preStopScript,omitempty"`
	PostStopScript      string               `yaml:"postStopScript,omitempty"`
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

type serviceLifecycle interface {
	Running() (bool, error)
	Start() error
	// Stop stops the service if it is running. The returned bool reports
	// whether this call actually issued a stop (true), versus finding it
	// already stopped and doing nothing (false).
	Stop() (bool, error)
}

var newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
	return NewDockerServiceFromContainerName(containerName, logger)
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
		foundTag := slices.Contains(p.Tags, tag)
		if !foundTag {
			return false
		}
	}
	return true
}

func (p Package) availableForTags(tags []string) bool {
	packageNeedsDocker := slices.Contains(p.Tags, "docker")
	requiredHasDocker := slices.Contains(tags, "docker")

	if packageNeedsDocker {
		if !requiredHasDocker {
			return false
		}
		return p.hasTags(tags)
	}

	filteredTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != "docker" {
			filteredTags = append(filteredTags, tag)
		}
	}
	return p.hasTags(filteredTags)
}

func (p Package) install(
	cfg Config,
	context string,
	opts map[string]bool,
	runHooks bool,
	registeredPorts PackagePortRegistry,
) (string, map[string]string, PackagePortRegistry, error) {
	cfg = p.withPackageTemplateVars(cfg, context, opts)
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
	// Run pre-flight checks
	for _, installStep := range p.InstallSteps {
		// Make sure only one install method is specified per install step
		if installStep.countInstallMethods() > 1 {
			return "", nil, nil, ErrMultipleInstallMethods
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.preflight(cfg, pkgName); err != nil {
				return "", nil, nil, fmt.Errorf("pre-flight check failed: %w", err)
			}
		}
	}
	// Pre-create dirs
	if err := os.MkdirAll(pkgCacheDir, fs.ModePerm); err != nil {
		return "", nil, nil, err
	}
	if err := os.MkdirAll(pkgContextDir, fs.ModePerm); err != nil {
		return "", nil, nil, err
	}
	if err := os.MkdirAll(pkgDataDir, fs.ModePerm); err != nil {
		return "", nil, nil, err
	}
	// Run pre-install script
	if runHooks && p.PreInstallScript != "" {
		if err := p.runHookScript(cfg, p.PreInstallScript); err != nil {
			return "", nil, nil, err
		}
	}
	// Perform install
	for _, installStep := range p.InstallSteps {
		// Evaluate condition if defined
		if installStep.Condition != "" {
			if ok, err := cfg.Template.EvaluateCondition(installStep.Condition, nil); err != nil {
				return "", nil, nil, NewInstallStepConditionError(
					installStep.Condition,
					err,
				)
			} else if !ok {
				cfg.Logger.Debug(
					"skipping install step due to condition: " + installStep.Condition,
				)
				continue
			}
		}
		if installStep.Docker != nil {
			var stepPorts ServicePortMap
			if registeredPorts != nil {
				stepPorts = registeredPorts[installStep.Docker.ContainerName]
			}
			if err := installStep.Docker.install(cfg, pkgName, stepPorts); err != nil {
				return "", nil, nil, err
			}
		} else if installStep.File != nil {
			if err := installStep.File.install(cfg, pkgName, p.filePath); err != nil {
				return "", nil, nil, err
			}
		} else if installStep.Config != nil {
			if err := installStep.Config.install(cfg, context, p.filePath); err != nil {
				return "", nil, nil, err
			}
		} else {
			return "", nil, nil, ErrNoInstallMethods
		}
	}
	// Capture port details for output templates
	retPorts, err := p.currentPorts(cfg, context)
	if err != nil {
		return "", nil, nil, err
	}
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"Ports": retPorts,
		},
	)
	retOutputs, err := p.renderOutputs(cfg, retPorts)
	if err != nil {
		return "", nil, nil, err
	}
	// Run post-install script
	if runHooks && p.PostInstallScript != "" {
		if err := p.runHookScript(cfg, p.PostInstallScript); err != nil {
			return "", nil, nil, err
		}
	}
	// Render notes and return
	var retNotes string
	if p.PostInstallNotes != "" {
		tmpNotes, err := cfg.Template.Render(p.PostInstallNotes, nil)
		if err != nil {
			return "", nil, nil, err
		}
		retNotes = tmpNotes
	}
	return retNotes, retOutputs, retPorts, nil
}

func (p Package) withPackageTemplateVars(
	cfg Config,
	context string,
	opts map[string]bool,
) Config {
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
				"BinDir":     cfg.BinDir,
				"CacheDir":   pkgCacheDir,
				"ContextDir": pkgContextDir,
				"DataDir":    pkgDataDir,
			},
			"System": map[string]string{
				"OS":   runtime.GOOS,
				"ARCH": runtime.GOARCH,
			},
		},
	)
	return cfg
}

func (p Package) currentPorts(
	cfg Config,
	context string,
) (PackagePortRegistry, error) {
	retPorts := make(PackagePortRegistry)
	tmpServices, err := p.services(cfg, context)
	if err != nil {
		return nil, err
	}
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	for _, svc := range tmpServices {
		shortContainerName := strings.TrimPrefix(svc.ContainerName, pkgName+`-`)
		tmpPortsContainer := make(ServicePortMap)
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
		retPorts[shortContainerName] = tmpPortsContainer
	}
	return retPorts, nil
}

func (p Package) renderOutputs(
	cfg Config,
	ports PackagePortRegistry,
) (map[string]string, error) {
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"Ports": ports,
		},
	)
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
			return nil, err
		}
		retOutputs[key] = val
	}
	return retOutputs, nil
}

func (p Package) uninstall(
	cfg Config,
	context string,
	keepData bool,
	runHooks bool,
) error {
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
					"skipping uninstall step due to condition: " + installStep.Condition,
				)
				continue
			}
		}
		// Make sure only one install method is specified per install step
		if installStep.countInstallMethods() > 1 {
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
		} else if installStep.Config != nil {
			if err := installStep.Config.uninstall(cfg, context); err != nil {
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
					"skipping install step due to condition: " + installStep.Condition,
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
		} else if installStep.Config != nil {
			if err := installStep.Config.activate(cfg, pkgName); err != nil {
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
					"skipping install step due to condition: " + installStep.Condition,
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
		} else if installStep.Config != nil {
			if err := installStep.Config.deactivate(cfg, pkgName); err != nil {
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
		return errors.New("package name cannot be empty")
	}
	// Check name matches allowed characters
	reName := regexp.MustCompile(`^[-a-zA-Z0-9]+$`)
	if !reName.Match([]byte(p.Name)) {
		return fmt.Errorf("invalid package name: %s", p.Name)
	}
	// Check empty version
	if p.Version == "" {
		return errors.New("package version cannot be empty")
	}
	// Check version is well formed
	if _, err := version.NewVersion(p.Version); err != nil {
		return fmt.Errorf("package version is malformed: %w", err)
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
		return fmt.Errorf(
			"package did not have expected file path: %s",
			expectedFilePath,
		)
	}
	// Validate install steps
	for _, installStep := range p.InstallSteps {
		// Evaluate condition if defined
		if installStep.Condition != "" {
			if _, err := cfg.Template.EvaluateCondition(installStep.Condition, nil); err != nil {
				return NewInstallStepConditionError(installStep.Condition, err)
			}
		}
		// Make sure only one install method is specified per install step
		if installStep.countInstallMethods() > 1 {
			return ErrMultipleInstallMethods
		}
		if installStep.Docker != nil {
			if err := installStep.Docker.validate(cfg); err != nil {
				return err
			}
		} else if installStep.File != nil {
			if err := installStep.File.validate(cfg); err != nil {
				return err
			}
		} else if installStep.Config != nil {
			if err := installStep.Config.validate(cfg); err != nil {
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

	// Run pre-start script
	if p.PreStartScript != "" {
		if err := p.runHookScript(cfg, p.PreStartScript); err != nil {
			return fmt.Errorf("pre-start hook failed: %w", err)
		}
	}
	var startErrors []string
	startedServices := make([]serviceLifecycle, 0)
	for _, step := range p.InstallSteps {
		if step.Docker != nil {
			if step.Docker.PullOnly {
				continue
			}
			containerName := fmt.Sprintf(
				"%s-%s",
				pkgName,
				step.Docker.ContainerName,
			)
			dockerService, err := newServiceFromContainerName(
				containerName,
				cfg.Logger,
			)
			if err != nil {
				startErrors = append(
					startErrors,
					fmt.Sprintf(
						"error initializing Docker service for container %s: %v",
						containerName,
						err,
					),
				)
				continue
			}
			wasRunning, err := dockerService.Running()
			if err != nil {
				startErrors = append(
					startErrors,
					fmt.Sprintf(
						"error checking Docker container status for %s: %v",
						containerName,
						err,
					),
				)
				continue
			}
			if wasRunning {
				continue
			}
			// Start the Docker container if it's not running
			slog.Info(
				"Starting Docker container " + containerName,
			)
			if err := dockerService.Start(); err != nil {
				startErrors = append(
					startErrors,
					fmt.Sprintf(
						"failed to start Docker container %s: %v",
						containerName,
						err,
					),
				)
				continue
			}
			startedServices = append(startedServices, dockerService)
		}
	}

	if len(startErrors) > 0 {
		p.rollbackStartedServices(startedServices)
		slog.Error(strings.Join(startErrors, "\n"))
		return ErrOperationFailed
	}

	// Run post-start script
	if p.PostStartScript != "" {
		if err := p.runHookScript(cfg, p.PostStartScript); err != nil {
			p.rollbackStartedServices(startedServices)
			return fmt.Errorf("post-start hook failed: %w", err)
		}
	}

	return nil
}

func (p Package) rollbackStartedServices(startedServices []serviceLifecycle) {
	if len(startedServices) == 0 {
		return
	}
	for idx := len(startedServices) - 1; idx >= 0; idx-- {
		if _, err := startedServices[idx].Stop(); err != nil {
			slog.Warn(
				fmt.Sprintf(
					"failed to roll back started service after start failure: %v",
					err,
				),
			)
		}
	}
}

func (p Package) stopService(cfg Config, context string) error {
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)

	// Run pre-stop script
	if p.PreStopScript != "" {
		if err := p.runHookScript(cfg, p.PreStopScript); err != nil {
			return fmt.Errorf("pre-stop hook failed: %w", err)
		}
	}

	var stopErrors []string
	stoppedServices := make([]serviceLifecycle, 0)
	for _, step := range p.InstallSteps {
		if step.Docker != nil {
			if step.Docker.PullOnly {
				continue
			}
			containerName := fmt.Sprintf(
				"%s-%s",
				pkgName,
				step.Docker.ContainerName,
			)
			dockerService, err := newServiceFromContainerName(
				containerName,
				cfg.Logger,
			)
			if err != nil {
				stopErrors = append(
					stopErrors,
					fmt.Sprintf(
						"error initializing Docker service for container %s: %v",
						containerName,
						err,
					),
				)
				continue
			}
			// Stop() itself checks whether the container is running before
			// acting, and reports whether it actually issued a stop. Relying
			// on that report - rather than a separate Running() check made
			// beforehand by this code - avoids a race where a concurrent,
			// unrelated operation stops the container in between the two
			// checks: this code would then see a "successful" no-op Stop()
			// and wrongly believe it owns a transition it never caused.
			stoppedNow, err := dockerService.Stop()
			if err != nil {
				// Don't track this service for rollback: a container found
				// stopped despite this error could have been stopped by a
				// concurrent, unrelated operation, and rolling it back would
				// wrongly undo that operation's intended effect.
				stopErrors = append(
					stopErrors,
					fmt.Sprintf(
						"failed to stop Docker container %s: %v",
						containerName,
						err,
					),
				)
				continue
			}
			if !stoppedNow {
				// The container was already stopped; nothing to do or roll
				// back, and no error since the desired end state holds.
				continue
			}
			slog.Info("Stopped container " + containerName)
			// Confirm the container is actually stopped - e.g. a restart
			// policy could bring it back up immediately after a successful
			// stop request.
			nowRunning, err := dockerService.Running()
			if err != nil {
				// This call's own Stop() reported success, so it owns the
				// transition even though the final state can't be
				// reconfirmed - track it for a best-effort rollback attempt.
				stoppedServices = append(stoppedServices, dockerService)
				stopErrors = append(
					stopErrors,
					fmt.Sprintf(
						"error checking Docker container status for %s: %v",
						containerName,
						err,
					),
				)
				continue
			}
			if nowRunning {
				stopErrors = append(
					stopErrors,
					fmt.Sprintf(
						"failed to stop Docker container %s: container is still running",
						containerName,
					),
				)
				continue
			}
			stoppedServices = append(stoppedServices, dockerService)
		}
	}

	if len(stopErrors) > 0 {
		p.rollbackStoppedServices(stoppedServices)
		slog.Error(strings.Join(stopErrors, "\n"))
		return ErrOperationFailed
	}

	// Run post-stop script
	if p.PostStopScript != "" {
		if err := p.runHookScript(cfg, p.PostStopScript); err != nil {
			p.rollbackStoppedServices(stoppedServices)
			return fmt.Errorf("post-stop hook failed: %w", err)
		}
	}

	return nil
}

func (p Package) rollbackStoppedServices(stoppedServices []serviceLifecycle) {
	if len(stoppedServices) == 0 {
		return
	}
	for idx := len(stoppedServices) - 1; idx >= 0; idx-- {
		if err := stoppedServices[idx].Start(); err != nil {
			slog.Warn(
				fmt.Sprintf(
					"failed to roll back stopped service: %v",
					err,
				),
			)
		}
	}
}

func (p Package) services(
	cfg Config,
	context string,
) ([]*DockerService, error) {
	var ret []*DockerService
	pkgName := fmt.Sprintf("%s-%s-%s", p.Name, p.Version, context)
	for _, step := range p.InstallSteps {
		if step.Docker != nil {
			if step.Docker.PullOnly {
				continue
			}
			containerName := fmt.Sprintf(
				"%s-%s",
				pkgName,
				step.Docker.ContainerName,
			)
			dockerService, err := NewDockerServiceFromContainerName(
				containerName,
				cfg.Logger,
			)
			if err != nil {
				cfg.Logger.Error(
					fmt.Sprintf(
						"error initializing Docker service for container %s: %v",
						containerName,
						err,
					),
				)
				return ret, ErrOperationFailed
			}
			ret = append(ret, dockerService)
		}
	}
	return ret, nil
}

func (p Package) runHookScript(cfg Config, hookScript string) error {
	renderedScript, err := cfg.Template.Render(hookScript, nil)
	if err != nil {
		return fmt.Errorf("failed to render hook script template: %w", err)
	}
	cmd := exec.Command("/bin/sh", "-c", renderedScript)
	// Wire the hook's stdio straight through to our own. Forwarding stdin is
	// what lets an interactive child process inherit our controlling terminal:
	// the cardano-node preInstall hook shells out to the mithril-client wrapper,
	// which runs "docker run -ti ...". Without a real stdin the nested TTY
	// allocation breaks, leaving the terminal in a bad state and orphaning the
	// long-running snapshot download in the background. See issue #239.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to run hook script: %w", err)
	}
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("run hook script exited with error: %w", err)
	}
	return nil
}

type PackageInstallStep struct {
	Condition string                    `yaml:"condition,omitempty"`
	Docker    *PackageInstallStepDocker `yaml:"docker,omitempty"`
	File      *PackageInstallStepFile   `yaml:"file,omitempty"`
	Config    *PackageInstallStepConfig `yaml:"config,omitempty"`
}

// countInstallMethods returns how many install method fields (docker, file,
// config) are set on this install step. Exactly one is required per step.
func (installStep PackageInstallStep) countInstallMethods() int {
	count := 0
	if installStep.Docker != nil {
		count++
	}
	if installStep.File != nil {
		count++
	}
	if installStep.Config != nil {
		count++
	}
	return count
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
		cfg.Logger.Debug("docker image missing")
		return errors.New("docker image must be provided")
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
		if errors.Is(err, ErrContainerNotExists) {
			// Container does not exist (we want this)
			return nil
		} else {
			return err
		}
	}
	return ErrContainerAlreadyExists
}

func (p *PackageInstallStepDocker) install(
	cfg Config,
	pkgName string,
	registeredPorts ServicePortMap,
) error {
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
	//nolint:prealloc
	var tmpCommand []string
	for _, cmd := range p.Command {
		tmpCmd, err := cfg.Template.Render(cmd, extraVars)
		if err != nil {
			return err
		}
		tmpCommand = append(tmpCommand, tmpCmd)
	}
	//nolint:prealloc
	var tmpArgs []string
	for _, arg := range p.Args {
		tmpArg, err := cfg.Template.Render(arg, extraVars)
		if err != nil {
			return err
		}
		tmpArgs = append(tmpArgs, tmpArg)
	}
	//nolint:prealloc
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
	//nolint:prealloc
	var tmpPorts []string
	portAllocations := make(ServicePortMap)
	for _, port := range p.Ports {
		tmpPort, err := cfg.Template.Render(port, extraVars)
		if err != nil {
			return err
		}
		tmpPort, err = ensureHostPortMapping(tmpPort, registeredPorts, portAllocations)
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

func (p *PackageInstallStepDocker) uninstall(
	cfg Config,
	pkgName string,
	keepData bool,
) error {
	if !p.PullOnly {
		containerName := fmt.Sprintf("%s-%s", pkgName, p.ContainerName)
		svc, err := NewDockerServiceFromContainerName(containerName, cfg.Logger)
		if err != nil {
			if errors.Is(err, ErrContainerNotExists) {
				cfg.Logger.Debug(
					"container missing on uninstall: " + containerName,
				)
			} else {
				return err
			}
		} else {
			if _, err := svc.Stop(); err != nil {
				return err
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

func (p *PackageInstallStepDocker) deactivate(
	cfg Config,
	pkgName string,
) error {
	// Nothing to do
	return nil
}

type PackageInstallStepFile struct {
	Binary      bool        `yaml:"binary"`
	Filename    string      `yaml:"filename"`
	Source      string      `yaml:"source"`
	Content     string      `yaml:"content"`
	Url         string      `yaml:"url"`
	Mode        fs.FileMode `yaml:"mode,omitempty"`
	Archive     string      `yaml:"archive,omitempty"`
	ArchivePath string      `yaml:"archivePath,omitempty"`
}

func (p *PackageInstallStepFile) validate(cfg Config) error {
	if p.Content == "" && p.Source == "" && p.Url == "" {
		cfg.Logger.Debug("file install step missing content, source, and url")
		return errors.New("packages must provide content, source, or url for file install types")
	}
	if p.Archive != "" {
		if p.Content != "" {
			cfg.Logger.Debug("archive combined with content")
			return errors.New("archive cannot be combined with content; use source or url")
		}
		if !validArchiveType(p.Archive) {
			cfg.Logger.Debug("unsupported archive type: " + p.Archive)
			return fmt.Errorf("unsupported archive type %q", p.Archive)
		}
		if p.ArchivePath == "" {
			cfg.Logger.Debug("archive set without archivePath")
			return errors.New("archivePath must be provided when archive is set")
		}
	}
	return nil
}

func (p *PackageInstallStepFile) install(
	cfg Config,
	pkgName string,
	packagePath string,
) error {
	tmpFilePath, err := cfg.Template.Render(p.Filename, nil)
	if err != nil {
		return err
	}
	filePath := filepath.Join(
		cfg.DataDir,
		pkgName,
		tmpFilePath,
	)
	filePath = filepath.Clean(filePath)
	expectedPrefix := filepath.Clean(filepath.Join(cfg.DataDir, pkgName)) + string(os.PathSeparator)
	if !strings.HasPrefix(filePath, expectedPrefix) {
		return fmt.Errorf("invalid file path %q: path traversal detected", filePath)
	}
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, fs.ModePerm); err != nil {
		return err
	}
	fileMode := fs.ModePerm
	if p.Mode > 0 {
		fileMode = p.Mode
	}
	fileContent, err := p.resolveContent(cfg, packagePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filePath, fileContent, fileMode); err != nil { //nolint:gosec // path traversal mitigated by prefix check above
		return err
	}
	cfg.Logger.Debug("wrote file " + filePath)
	return nil
}

// resolveContent renders this file's content from whichever of content,
// source, or url is set, extracting an archive member first when Archive is
// set. It holds the logic shared by the file and config install step types,
// which differ only in their destination path and overwrite behavior.
func (p *PackageInstallStepFile) resolveContent(
	cfg Config,
	packagePath string,
) ([]byte, error) {
	var fileContent []byte
	if p.Content != "" {
		tmpContent, err := cfg.Template.Render(p.Content, nil)
		if err != nil {
			return nil, err
		}
		fileContent = []byte(tmpContent)
	} else if p.Source != "" {
		fullSourcePath := filepath.Join(
			filepath.Dir(packagePath),
			p.Source,
		)
		if p.Archive != "" {
			// Archive data is binary and must not be interpreted as a template.
			// Bound the read the same way as url-sourced downloads, so a
			// malformed or oversized package asset can't be fully buffered
			// into memory before the archive-entry limit even runs.
			sourceFile, err := os.Open(fullSourcePath)
			if err != nil {
				return nil, err
			}
			defer sourceFile.Close()
			tmpContentBytes, err := readArchiveEntry(sourceFile, fullSourcePath, maxDownloadSize)
			if err != nil {
				return nil, fmt.Errorf("failed to read %q: %w", fullSourcePath, err)
			}
			fileContent = tmpContentBytes
		} else {
			tmpContentBytes, err := os.ReadFile(fullSourcePath)
			if err != nil {
				return nil, err
			}
			tmpContent, err := cfg.Template.Render(string(tmpContentBytes), nil)
			if err != nil {
				return nil, err
			}
			fileContent = []byte(tmpContent)
		}
	} else if p.Url != "" {
		tmpUrl, err := cfg.Template.Render(p.Url, nil)
		if err != nil {
			return nil, err
		}

		// Validate URL
		u, err := url.Parse(tmpUrl)
		if err != nil {
			return nil, err
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, errors.New("invalid URL given")
		}

		// Fetch data
		ctx := context.Background()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, tmpUrl, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL is validated above
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("nil response for URL: %s", tmpUrl)
		}
		defer resp.Body.Close()
		respBody, err := readArchiveEntry(resp.Body, tmpUrl, maxDownloadSize)
		if err != nil {
			return nil, fmt.Errorf("failed to download %q: %w", tmpUrl, err)
		}

		fileContent = respBody
	} else {
		return nil, errors.New("packages must provide content, source, or url for file install types")
	}
	if p.Archive != "" {
		tmpArchivePath, err := cfg.Template.Render(p.ArchivePath, nil)
		if err != nil {
			return nil, err
		}
		fileContent, err = extractArchiveFile(p.Archive, tmpArchivePath, fileContent)
		if err != nil {
			return nil, fmt.Errorf("failed to extract %q from archive: %w", tmpArchivePath, err)
		}
	}
	return fileContent, nil
}

func (p *PackageInstallStepFile) uninstall(cfg Config, pkgName string) error {
	filePath := filepath.Join(
		cfg.DataDir,
		pkgName,
		p.Filename,
	)
	cfg.Logger.Debug("deleting file " + filePath)
	if err := os.Remove(filePath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			cfg.Logger.Warn("failed to remove file " + filePath)
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
		cfg.Logger.Debug(
			fmt.Sprintf("wrote symlink from %s to %s", binPath, filePath),
		)
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
		cfg.Logger.Debug("removed symlink " + binPath + "for " + pkgName)
	}
	return nil
}

// PackageInstallStepConfig manages a configuration file. Unlike the file
// install step, a config file lives in the context-level directory (shared
// and persisted across the package's own versions, rather than the
// per-version package data directory) and, once written, is never
// overwritten by a later install. This lets a package upgrade change its
// rendered defaults without clobbering a config file the user has hand-edited
// since the initial install. See issue #567.
type PackageInstallStepConfig struct {
	Filename    string      `yaml:"filename"`
	Source      string      `yaml:"source,omitempty"`
	Content     string      `yaml:"content,omitempty"`
	Url         string      `yaml:"url,omitempty"`
	Mode        fs.FileMode `yaml:"mode,omitempty"`
	Archive     string      `yaml:"archive,omitempty"`
	ArchivePath string      `yaml:"archivePath,omitempty"`
}

// asFile adapts this config step to a PackageInstallStepFile so it can reuse
// the file step's content-source validation and resolution.
func (p *PackageInstallStepConfig) asFile() *PackageInstallStepFile {
	return &PackageInstallStepFile{
		Content:     p.Content,
		Source:      p.Source,
		Url:         p.Url,
		Archive:     p.Archive,
		ArchivePath: p.ArchivePath,
	}
}

func (p *PackageInstallStepConfig) validate(cfg Config) error {
	if p.Filename == "" {
		cfg.Logger.Debug("config file missing filename")
		return errors.New("filename must be provided for config install types")
	}
	return p.asFile().validate(cfg)
}

func (p *PackageInstallStepConfig) install(
	cfg Config,
	context string,
	packagePath string,
) error {
	tmpFilePath, err := cfg.Template.Render(p.Filename, nil)
	if err != nil {
		return err
	}
	pkgContextDir := filepath.Join(cfg.DataDir, context)
	if err := os.MkdirAll(pkgContextDir, fs.ModePerm); err != nil {
		return err
	}
	relFilePath := filepath.Clean(tmpFilePath)
	displayPath := filepath.Join(pkgContextDir, relFilePath)
	// Open the context directory as an os.Root and do every subsequent
	// filesystem operation through it. Unlike a lexical prefix check on the
	// resolved path, Root rejects any component - including a symlink placed
	// inside the context directory by a prior install step - that would
	// resolve outside pkgContextDir, so a malicious or buggy filename can't
	// escape it even via an intermediate symlink.
	root, err := os.OpenRoot(pkgContextDir)
	if err != nil {
		return err
	}
	defer root.Close()
	// Preserve an existing config file rather than overwriting it. This is
	// what lets a package upgrade run install() again - via Upgrade(), which
	// uninstalls the old version and installs the new one - without losing
	// any changes the user made to the file after the initial install.
	if _, err := root.Lstat(relFilePath); err == nil {
		cfg.Logger.Debug("preserving existing config file " + displayPath)
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("invalid file path %q: %w", displayPath, err)
	}
	if parentDir := filepath.Dir(relFilePath); parentDir != "." {
		if err := root.MkdirAll(parentDir, fs.ModePerm); err != nil {
			return fmt.Errorf("invalid file path %q: %w", displayPath, err)
		}
	}
	fileMode := fs.ModePerm
	if p.Mode > 0 {
		fileMode = p.Mode
	}
	fileContent, err := p.asFile().resolveContent(cfg, packagePath)
	if err != nil {
		return err
	}
	f, err := root.OpenFile(
		relFilePath,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		fileMode,
	)
	if err != nil {
		return fmt.Errorf("invalid file path %q: %w", displayPath, err)
	}
	defer f.Close()
	if _, err := f.Write(fileContent); err != nil {
		return err
	}
	cfg.Logger.Debug("wrote config file " + displayPath)
	return nil
}

// uninstall intentionally leaves the config file in place. It is never
// recreated by install() once it exists, so deleting it here - whether for a
// real uninstall or as the first half of an Upgrade() - would either destroy
// user changes or force every upgrade to fall back to the package's rendered
// defaults, defeating the point of this install step type. A user who wants
// the file gone can remove it manually.
func (p *PackageInstallStepConfig) uninstall(cfg Config, context string) error {
	return nil
}

func (p *PackageInstallStepConfig) activate(cfg Config, pkgName string) error {
	// Nothing to do
	return nil
}

func (p *PackageInstallStepConfig) deactivate(cfg Config, pkgName string) error {
	// Nothing to do
	return nil
}

func ensureHostPortMapping(
	rawPort string,
	registered ServicePortMap,
	allocated ServicePortMap,
) (string, error) {
	portMappings, err := nat.ParsePortSpec(rawPort)
	if err != nil {
		return "", err
	}
	if len(portMappings) != 1 {
		return "", fmt.Errorf("port spec %q expanded to %d mappings; ranges are not supported", rawPort, len(portMappings))
	}
	portMapping := portMappings[0]
	containerPort := portMapping.Port.Port()
	if containerPort == "" {
		return rawPort, nil
	}
	proto := portMapping.Port.Proto()
	hostPort := portMapping.Binding.HostPort
	if hostPort == "" && len(registered) > 0 {
		if registeredPort, ok := registered[containerPort]; ok && registeredPort != "" {
			hostPort = registeredPort
		}
	}
	if hostPort == "" {
		tmpPort, err := allocateEphemeralPort(proto)
		if err != nil {
			return "", err
		}
		hostPort = tmpPort
	}
	if allocated != nil {
		if allocated[containerPort] == "" {
			allocated[containerPort] = hostPort
		}
	}
	newPort := formatPortSpec(
		portMapping.Binding.HostIP,
		hostPort,
		containerPort,
		proto,
	)
	return newPort, nil
}

func allocateEphemeralPort(proto string) (string, error) {
	switch strings.ToLower(proto) {
	case "", "tcp":
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		if listener == nil {
			return "", errors.New("failed to allocate TCP port")
		}
		defer listener.Close()
		addr := listener.Addr().(*net.TCPAddr) //nolint:forcetypeassert
		return strconv.Itoa(addr.Port), nil
	case "udp":
		conn, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		if conn == nil {
			return "", errors.New("failed to allocate UDP port")
		}
		defer conn.Close()
		addr := conn.LocalAddr().(*net.UDPAddr) //nolint:forcetypeassert
		return strconv.Itoa(addr.Port), nil
	default:
		return "", fmt.Errorf("unsupported protocol %q for port allocation", proto)
	}
}

func formatPortSpec(
	ip string,
	hostPort string,
	containerPort string,
	proto string,
) string {
	var sb strings.Builder
	if ip != "" {
		sb.WriteString(ip)
		sb.WriteString(":")
	}
	if hostPort != "" || ip != "" {
		sb.WriteString(hostPort)
		sb.WriteString(":")
	}
	sb.WriteString(containerPort)
	if proto != "" {
		sb.WriteString("/")
		sb.WriteString(proto)
	}
	return sb.String()
}
