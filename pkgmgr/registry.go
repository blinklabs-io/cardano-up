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

var RegistryPackages = []Package{
	{
		Name:        "cardano-node",
		Version:     "8.7.3",
		Description: "Cardano node software by Input Output Global",
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
						"{{ .Paths.CacheDir }}/ipc:/ipc",
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

	// Test packages
	{
		Name:             "test-packageA",
		Version:          "1.0.2",
		PostInstallNotes: "Notes for {{ .Package.Name }}",
	},
	{
		Name:             "test-packageA",
		Version:          "1.0.3",
		PostInstallNotes: "Notes for {{ .Package.Name }}",
	},
	{
		Name:    "test-packageA",
		Version: "2.1.3",
	},
	{
		Name:    "test-packageB",
		Version: "0.1.0",
		Dependencies: []string{
			"test-packageA < 2.0.0, >= 1.0.2",
		},
	},
}
