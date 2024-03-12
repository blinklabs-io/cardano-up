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
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var registryBuiltinPackages = []Package{
	{
		Name:        "cardano-node",
		Version:     "8.7.3",
		Description: "Cardano node software by Input Output Global",
		Tags:        []string{"docker", "linux", "darwin", "amd64", "arm64"},
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "cardano-node",
					Image:         "ghcr.io/blinklabs-io/cardano-node:8.7.3",
					Env: map[string]string{
						"NETWORK":                  "{{ .Context.Network }}",
						"CARDANO_NODE_SOCKET_PATH": "/ipc/node.socket",
					},
					Binds: []string{
						"{{ .Paths.ContextDir }}/node-ipc:/ipc",
						"{{ .Paths.DataDir }}/data:/data",
					},
					Ports: []string{
						"3001",
					},
				},
			},
			{
				File: &PackageInstallStepFile{
					Binary:   true,
					Filename: "cardano-cli",
					// TODO: figure out how to get network magic for named network
					Content: `#!/bin/bash
docker exec -ti {{ .Package.Name }}-cardano-node cardano-cli $@
`,
				},
			},
		},
	},
	{
		Name:        "mithril-client",
		Version:     "0.5.17",
		Description: "Mithril client by Input Output Global",
		Tags:        []string{"docker", "linux", "darwin", "amd64", "arm64"},
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "mithril-client",
					Image:         "ghcr.io/blinklabs-io/mithril-client:0.5.17-1",
					PullOnly:      true,
				},
			},
			{
				File: &PackageInstallStepFile{
					Filename: "mithril-client",
					Content: `#!/bin/bash
docker run --rm -ti ghcr.io/blinklabs-io/mithril-client:0.5.17 $@
`,
				},
			},
		},
	},
	{
		Name:        "mithril-client",
		Version:     "0.7.0",
		Description: "Mithril client by Input Output Global",
		Tags:        []string{"docker", "linux", "darwin", "amd64", "arm64"},
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "mithril-client",
					Image:         "ghcr.io/blinklabs-io/mithril-client:0.7.0-1",
					PullOnly:      true,
				},
			},
			{
				File: &PackageInstallStepFile{
					Filename: "mithril-client",
					Content: `#!/bin/bash
docker run --rm -ti ghcr.io/blinklabs-io/mithril-client:0.7.0-1 $@
`,
				},
			},
		},
	},
	{
		Name:        "ogmios",
		Version:     "v6.1.0",
		Description: "Ogmios, a WebSocket & HTTP server for Cardano, providing a bridge between Cardano nodes and clients.",
		Tags:        []string{"docker", "linux", "darwin", "amd64", "arm64"},
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "ogmios",
					Image:         "cardanosolutions/ogmios:v6.1.0",
					Binds: []string{
						"{{ .Paths.ContextDir }}/node-ipc:/ipc",
					},
					Ports: []string{
						"1337",
					},
					Command: []string{
						"ogmios",
						"--log-level", "info",
						"--host", "0.0.0.0",
						"--port", "1337",
						"--node-socket", "/ipc/node.socket",
						"--node-config", "/config/mainnet/cardano-node/config.json",
					},
				},
			},
		},
	},

	// Test packages
	{
		Name:             "test-packageA",
		Version:          "1.0.2",
		Tags:             []string{"docker", "linux", "darwin", "amd64", "arm64"},
		PostInstallNotes: "Notes for {{ .Package.Name }}",
	},
	{
		Name:             "test-packageA",
		Version:          "1.0.3",
		Tags:             []string{"docker", "linux", "darwin", "amd64", "arm64"},
		PostInstallNotes: "Notes for {{ .Package.Name }}",
		Outputs: []PackageOutput{
			{
				Name:        "foo",
				Description: "the 'foo' description",
				Value:       `{{ .Package.Name }}`,
			},
		},
	},
	{
		Name:    "test-packageA",
		Version: "2.1.3",
		Tags:    []string{"docker", "linux", "darwin", "amd64", "arm64"},
	},
	{
		Name:    "test-packageB",
		Version: "0.1.0",
		Tags:    []string{"docker", "linux", "darwin", "amd64", "arm64"},
		Dependencies: []string{
			"test-packageA[fooA,-fooB] < 2.0.0, >= 1.0.2",
		},
		PostInstallNotes: "Values:\n\n{{ toPrettyJson . }}",
		InstallSteps: []PackageInstallStep{
			{
				Condition: `eq .Package.ShortName "test-packageB"`,
				File: &PackageInstallStepFile{
					Filename: "test-file1",
					Content:  `test1`,
				},
			},
			{
				Condition: `eq .Package.ShortName "test-packageZ"`,
				File: &PackageInstallStepFile{
					Filename: "test-file2",
					Content:  `test2`,
				},
			},
		},
	},
}

func registryPackages(cfg Config) ([]Package, error) {
	if cfg.RegistryDir != "" {
		return registryPackagesDir(cfg)
	} else if cfg.RegistryUrl != "" {
		return registryPackagesUrl(cfg)
	} else {
		return registryBuiltinPackages[:], nil
	}
}

func registryPackagesDir(cfg Config) ([]Package, error) {
	tmpFs := os.DirFS("/").(fs.ReadFileFS)
	return registryPackagesFs(cfg, tmpFs)
}

func registryPackagesFs(cfg Config, filesystem fs.ReadFileFS) ([]Package, error) {
	var ret []Package
	err := fs.WalkDir(
		filesystem,
		cfg.RegistryDir,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Skip dirs
			if d.IsDir() {
				// Skip all files inside dot-dirs
				if strings.HasPrefix(d.Name(), `.`) && d.Name() != `.` {
					return fs.SkipDir
				}
				return nil
			}
			// Skip non-YAML files based on file extension
			if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
				return nil
			}
			// Try to parse YAML file as package
			fileData, err := filesystem.ReadFile(path)
			if err != nil {
				return err
			}
			var tmpPkg Package
			if err := yaml.Unmarshal(fileData, &tmpPkg); err != nil {
				cfg.Logger.Warn(
					fmt.Sprintf(
						"failed to load %q as package: %s",
						path,
						err,
					),
				)
				return nil
			}
			ret = append(ret, tmpPkg)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func registryPackagesUrl(cfg Config) ([]Package, error) {
	cachePath := filepath.Join(
		cfg.CacheDir,
		"registry",
	)
	// Check age of existing cache
	stat, err := os.Stat(cachePath)
	if err != nil {
		if err != fs.ErrNotExist {
			return nil, err
		}
	}
	// Fetch and extract registry ZIP into cache if it doesn't exist or is too old
	if err == fs.ErrNotExist ||
		stat.ModTime().Before(time.Now().Add(-24*time.Hour)) {
		// Fetch registry ZIP
		resp, err := http.Get(cfg.RegistryUrl)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		zipData := bytes.NewReader(respBody)
		zipReader, err := zip.NewReader(
			zipData,
			int64(zipData.Len()),
		)
		if err != nil {
			return nil, err
		}
		// Clear out existing cache files
		if err := os.RemoveAll(cachePath); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(cachePath, fs.ModePerm); err != nil {
			return nil, err
		}
		// Extract files from ZIP into cache path
		for _, zipFile := range zipReader.File {
			outPath := filepath.Join(
				cachePath,
				zipFile.Name,
			)
			// Create parent dir
			if err := os.MkdirAll(filepath.Dir(outPath), fs.ModePerm); err != nil {
				return nil, err
			}
			// Read file bytes
			zf, err := zipFile.Open()
			if err != nil {
				return nil, err
			}
			zfData, err := io.ReadAll(zf)
			if err != nil {
				return nil, err
			}
			zf.Close()
			// Write file
			if err := os.WriteFile(outPath, zfData, fs.ModePerm); err != nil {
				return nil, err
			}
		}
	}
	// Process cache dir
	cfg.RegistryDir = cachePath
	return registryPackagesDir(cfg)
}
