// Copyright 2026 Blink Labs Software
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
	"io"
	"log/slog"
	"testing"
)

// TestRefreshInstalledPackageRuntimeUpdatesPortOutputs checks that outputs
// captured from a Docker-managed dynamic port are refreshed after startup.
func TestRefreshInstalledPackageRuntimeUpdatesPortOutputs(t *testing.T) {
	pkg := Package{
		Name:    "dynamic",
		Version: "1.0.0",
		Outputs: []PackageOutput{
			{
				Name:  "port",
				Value: `{{ index (index .Ports "api") "8080" }}`,
			},
		},
	}
	installedPkg := InstalledPackage{
		Package: pkg,
		Context: "default",
		Options: map[string]bool{},
		Outputs: map[string]string{
			"DYNAMIC_PORT": "55940",
		},
	}
	pm := &PackageManager{
		config: Config{
			Template: NewTemplate(nil),
		},
		state: &State{
			PortRegistry: make(PortRegistry),
		},
	}
	cfg := pkg.withPackageTemplateVars(
		pm.config,
		installedPkg.Context,
		installedPkg.Options,
	)
	currentPorts := PackagePortRegistry{
		"api": {
			"8080": "55964",
		},
	}

	if err := pm.refreshInstalledPackageRuntime(
		&installedPkg,
		cfg,
		currentPorts,
	); err != nil {
		t.Fatalf("unexpected refresh error: %s", err)
	}
	if got, want := installedPkg.Outputs["DYNAMIC_PORT"], "55964"; got != want {
		t.Fatalf("unexpected refreshed output: got %q, want %q", got, want)
	}
	if got, want := pm.state.PortRegistry["default"]["dynamic"]["api"]["8080"], "55964"; got != want {
		t.Fatalf("unexpected registered port: got %q, want %q", got, want)
	}
}

// TestUpContinuesAfterFailureAndPersistsRefreshes checks that package hooks
// receive package template variables, a package after a startup failure is
// still refreshed, and dependent outputs see persisted values through .Env.
func TestUpContinuesAfterFailureAndPersistsRefreshes(t *testing.T) {
	cfg := Config{
		ConfigDir: t.TempDir(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template:  NewTemplate(nil),
	}
	pm := &PackageManager{
		config: cfg,
		state: &State{
			config:        cfg,
			ActiveContext: "default",
			Contexts: map[string]Context{
				"default": {},
			},
			InstalledPackages: []InstalledPackage{
				{
					Package: Package{
						Name:           "successful",
						Version:        "1.0.0",
						PreStartScript: `{{ if .Package.Options.enabled }}true{{ else }}exit 1{{ end }}`,
						Outputs: []PackageOutput{
							{
								Name:  "value",
								Value: "refreshed",
							},
						},
					},
					Context: "default",
					Options: map[string]bool{
						"enabled": true,
					},
					Outputs: map[string]string{
						"SUCCESSFUL_VALUE": "stale",
					},
				},
				{
					Package: Package{
						Name:           "failing",
						Version:        "1.0.0",
						PreStartScript: "exit 1",
					},
					Context: "default",
				},
				{
					Package: Package{
						Name:    "dependent",
						Version: "1.0.0",
						Outputs: []PackageOutput{
							{
								Name:  "value",
								Value: `{{ .Env.SUCCESSFUL_VALUE }}`,
							},
						},
					},
					Context: "default",
					Outputs: map[string]string{
						"DEPENDENT_VALUE": "stale",
					},
				},
			},
			PortRegistry: make(PortRegistry),
		},
	}

	if err := pm.Up(); err == nil {
		t.Fatal("expected startup error from failing package")
	}
	if got, want := pm.state.InstalledPackages[0].Outputs["SUCCESSFUL_VALUE"], "refreshed"; got != want {
		t.Fatalf("unexpected in-memory output: got %q, want %q", got, want)
	}
	if got, want := pm.state.InstalledPackages[2].Outputs["DEPENDENT_VALUE"], "refreshed"; got != want {
		t.Fatalf("unexpected dependent in-memory output: got %q, want %q", got, want)
	}

	reloadedState := NewState(cfg)
	if err := reloadedState.Load(); err != nil {
		t.Fatalf("failed to reload persisted state: %s", err)
	}
	if got, want := reloadedState.InstalledPackages[0].Outputs["SUCCESSFUL_VALUE"], "refreshed"; got != want {
		t.Fatalf("unexpected persisted output: got %q, want %q", got, want)
	}
	if got, want := reloadedState.InstalledPackages[2].Outputs["DEPENDENT_VALUE"], "refreshed"; got != want {
		t.Fatalf("unexpected dependent persisted output: got %q, want %q", got, want)
	}
}
