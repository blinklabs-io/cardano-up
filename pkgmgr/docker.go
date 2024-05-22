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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

const (
	dockerInstallError = `could not contact Docker daemon

Docker is required to be already installed and running. Please refer to the following pages for more information
about how to install Docker.

 * https://docs.docker.com/get-docker/
 * https://docs.docker.com/engine/install/

If Docker is already installed but the socket is not in a standard location, you can use the DOCKER_HOST environment
variable to point to it.
`
)

type DockerService struct {
	client        *client.Client
	logger        *slog.Logger
	ContainerId   string
	ContainerName string
	Image         string
	Env           map[string]string
	Command       []string
	Args          []string
	Binds         []string
	Ports         []string
}

func NewDockerServiceFromContainerName(
	containerName string,
	logger *slog.Logger,
) (*DockerService, error) {
	ret := &DockerService{
		logger: logger,
	}
	client, err := ret.getClient()
	if err != nil {
		return nil, err
	}
	tmpContainers, err := client.ContainerList(
		context.Background(),
		container.ListOptions{
			All: true,
		},
	)
	if err != nil {
		return nil, err
	}
	for _, tmpContainer := range tmpContainers {
		for _, tmpContainerName := range tmpContainer.Names {
			tmpContainerName = strings.TrimPrefix(tmpContainerName, `/`)
			if tmpContainerName == containerName {
				ret.ContainerId = tmpContainer.ID
				if err := ret.refresh(); err != nil {
					return nil, err
				}
				return ret, nil
			}
		}
	}
	return nil, ErrContainerNotExists
}

func (d *DockerService) Running() (bool, error) {
	container, err := d.inspect()
	if err != nil {
		return false, err
	}
	return container.State.Running, nil
}

func (d *DockerService) Start() error {
	running, err := d.Running()
	if err != nil {
		return err
	}
	if !running {
		client, err := d.getClient()
		if err != nil {
			return err
		}
		d.logger.Debug(fmt.Sprintf("starting container %s", d.ContainerName))
		if err := client.ContainerStart(
			context.Background(),
			d.ContainerId,
			container.StartOptions{},
		); err != nil {
			return err
		}
	}
	return nil
}

func (d *DockerService) Stop() error {
	running, err := d.Running()
	if err != nil {
		return err
	}
	if running {
		client, err := d.getClient()
		if err != nil {
			return err
		}
		d.logger.Debug(fmt.Sprintf("stopping container %s", d.ContainerName))
		stopTimeout := 60
		if err := client.ContainerStop(
			context.Background(),
			d.ContainerId,
			container.StopOptions{
				Timeout: &stopTimeout,
			},
		); err != nil {
			return err
		}
	}
	return nil
}

func (d *DockerService) Create() error {
	client, err := d.getClient()
	if err != nil {
		return err
	}
	if err := d.pullImage(); err != nil {
		return err
	}
	// Convert env
	var tmpEnv []string
	for k, v := range d.Env {
		tmpEnv = append(
			tmpEnv,
			fmt.Sprintf("%s=%s", k, v),
		)
	}
	sort.Strings(tmpEnv)
	// Convert ports
	_, tmpPorts, err := nat.ParsePortSpecs(d.Ports)
	if err != nil {
		return err
	}
	// Set the desired user ID and group ID
	userID := os.Getuid()
	groupID := os.Getgid()
	userAndGroup := fmt.Sprintf("%d:%d", userID, groupID)
	// Create container
	d.logger.Debug(fmt.Sprintf("creating container %s", d.ContainerName))
	resp, err := client.ContainerCreate(
		context.Background(),
		&container.Config{
			Hostname:   d.ContainerName,
			Image:      d.Image,
			Entrypoint: d.Command,
			Cmd:        d.Args,
			Env:        tmpEnv[:],
			User:       userAndGroup,
		},
		&container.HostConfig{
			RestartPolicy: container.RestartPolicy{
				Name: container.RestartPolicyUnlessStopped,
			},
			Binds:        d.Binds[:],
			PortBindings: tmpPorts,
		},
		nil,
		nil,
		d.ContainerName,
	)
	if err != nil {
		return err
	}
	d.ContainerId = resp.ID
	for _, warning := range resp.Warnings {
		d.logger.Warn(warning)
	}
	return nil
}

func (d *DockerService) Remove() error {
	running, err := d.Running()
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("can't remove a running container")
	}
	client, err := d.getClient()
	if err != nil {
		return err
	}
	d.logger.Debug(fmt.Sprintf("removing container %s", d.ContainerName))
	if err := client.ContainerRemove(
		context.Background(),
		d.ContainerId,
		container.RemoveOptions{},
	); err != nil {
		return err
	}
	return nil
}

func (d *DockerService) Logs(
	follow bool,
	tail string,
	stdoutWriter io.Writer,
	stderrWriter io.Writer,
) error {
	client, err := d.getClient()
	if err != nil {
		return err
	}
	logsOut, err := client.ContainerLogs(
		context.Background(),
		d.ContainerName,
		container.LogsOptions{
			Follow:     follow,
			Tail:       tail,
			ShowStdout: true,
			ShowStderr: true,
		},
	)
	if err != nil {
		return err
	}
	defer logsOut.Close()
	if _, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, logsOut); err != nil {
		if err != io.EOF {
			return err
		}
	}
	return nil
}

func (d *DockerService) pullImage() error {
	client, err := d.getClient()
	if err != nil {
		return err
	}
	out, err := client.ImagePull(
		context.Background(),
		d.Image,
		image.PullOptions{},
	)
	if err != nil {
		return err
	}
	defer out.Close()
	// Log pull progress
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		var tmpStatus struct {
			Status         string         `json:"status"`
			ProgressDetail map[string]any `json:"progressDetail"`
			Id             string         `json:"id"`
		}
		line := scanner.Text()
		if err := json.Unmarshal([]byte(line), &tmpStatus); err != nil {
			d.logger.Warn(
				fmt.Sprintf(
					"failed to unmarshal docker image pull status update: %s",
					err,
				),
			)
		}
		// Skip progress update lines
		if len(tmpStatus.ProgressDetail) > 0 {
			continue
		}
		if tmpStatus.Id == "" {
			d.logger.Info(tmpStatus.Status)
		} else {
			d.logger.Info(
				fmt.Sprintf(
					"%s: %s",
					tmpStatus.Id,
					tmpStatus.Status,
				),
			)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (d *DockerService) inspect() (types.ContainerJSON, error) {
	client, err := d.getClient()
	if err != nil {
		return types.ContainerJSON{}, err
	}
	container, err := client.ContainerInspect(
		context.Background(),
		d.ContainerId,
	)
	if err != nil {
		return types.ContainerJSON{}, err
	}
	return container, nil
}

func (d *DockerService) refresh() error {
	container, err := d.inspect()
	if err != nil {
		return err
	}
	d.ContainerName = strings.TrimPrefix(container.Name, `/`)
	d.Image = container.Config.Image
	d.Env = make(map[string]string)
	for _, tmpEnv := range container.Config.Env {
		envVarParts := strings.SplitN(tmpEnv, `=`, 2)
		if envVarParts != nil {
			envVarName, envVarValue := envVarParts[0], envVarParts[1]
			d.Env[envVarName] = envVarValue
		}
	}
	d.Command = container.Config.Entrypoint[:]
	d.Args = container.Config.Cmd[:]
	var tmpBinds []string
	for _, mount := range container.Mounts {
		if mount.Type != "bind" {
			continue
		}
		tmpRoRwFlag := "ro"
		if mount.RW {
			tmpRoRwFlag = "rw"
		}
		tmpBind := fmt.Sprintf(
			"%s:%s:%s",
			mount.Source,
			mount.Destination,
			tmpRoRwFlag,
		)
		tmpBinds = append(tmpBinds, tmpBind)
	}
	d.Binds = tmpBinds[:]
	var tmpPorts []string
	for port, portBindings := range container.NetworkSettings.Ports {
		// Skip exposed container ports without a mapping
		if len(portBindings) == 0 {
			continue
		}
		tmpPort := fmt.Sprintf(
			"0.0.0.0:%s:%s",
			portBindings[0].HostPort,
			port.Port(),
		)
		tmpPorts = append(tmpPorts, tmpPort)
	}
	d.Ports = tmpPorts[:]
	return nil
}

func (d *DockerService) getClient() (*client.Client, error) {
	if d.client == nil {
		tmpClient, err := NewDockerClient()
		if err != nil {
			return nil, err
		}
		d.client = tmpClient
	}
	return d.client, nil
}

func NewDockerClient() (*client.Client, error) {
	clientOpts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}
	// Determine Docker socket path if env override isn't set
	if _, ok := os.LookupEnv("DOCKER_HOST"); !ok {
		// Determine fallback path for socket on Docker Desktop for Mac
		userHomeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		fallbackSocketPath := filepath.Join(
			userHomeDir,
			".docker",
			"run",
			"docker.sock",
		)
		if _, err := os.Stat(client.DefaultDockerHost); err == nil {
			// Explicitly set the host to the default Docker socket path
			clientOpts = append(
				clientOpts,
				client.WithHost(
					`unix://`+client.DefaultDockerHost,
				),
			)
		} else if _, err := os.Stat(fallbackSocketPath); err == nil {
			// Set the host to the fallback socket path
			clientOpts = append(
				clientOpts,
				client.WithHost(
					`unix://`+fallbackSocketPath,
				),
			)
		}
	}
	tmpClient, err := client.NewClientWithOpts(clientOpts...)
	if err != nil {
		return nil, err
	}
	return tmpClient, nil
}

func CheckDockerConnectivity() error {
	if _, err := NewDockerClient(); err != nil {
		return errors.New(dockerInstallError)
	}
	return nil
}

func RemoveDockerImage(imageName string) error {
	client, err := NewDockerClient()
	if err != nil {
		return err
	}
	_, err = client.ImageRemove(
		context.Background(),
		imageName,
		image.RemoveOptions{},
	)
	if err != nil {
		return err
	}
	return nil
}
