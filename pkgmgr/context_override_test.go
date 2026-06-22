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
	"errors"
	"testing"
)

func newTestPackageManager() *PackageManager {
	return &PackageManager{
		config: Config{},
		state: &State{
			ActiveContext: "default",
			Contexts: map[string]Context{
				"default": {Network: "preprod"},
				"other":   {Network: "preview"},
			},
			InstalledPackages: []InstalledPackage{
				{
					Package: Package{Name: "foo", Version: "1.0.0"},
					Context: "default",
					Outputs: map[string]string{"FOO": "default"},
				},
				{
					Package: Package{Name: "bar", Version: "2.0.0"},
					Context: "other",
					Outputs: map[string]string{"BAR": "other"},
				},
			},
		},
	}
}

func TestInstalledPackagesUsesActiveContextByDefault(t *testing.T) {
	pm := newTestPackageManager()
	pkgs := pm.InstalledPackages()
	if len(pkgs) != 1 || pkgs[0].Package.Name != "foo" {
		t.Fatalf(
			"expected only the active context (default) package, got: %#v",
			pkgs,
		)
	}
}

func TestSetActiveContextOverrideTargetsOtherContext(t *testing.T) {
	pm := newTestPackageManager()
	if err := pm.SetActiveContextOverride("other"); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// InstalledPackages now reflects the override
	pkgs := pm.InstalledPackages()
	if len(pkgs) != 1 || pkgs[0].Package.Name != "bar" {
		t.Fatalf(
			"expected only the overridden context (other) package, got: %#v",
			pkgs,
		)
	}
	// EffectiveContext reflects the override
	name, ctx := pm.EffectiveContext()
	if name != "other" || ctx.Network != "preview" {
		t.Fatalf("expected effective context other/preview, got %q/%q", name, ctx.Network)
	}
	// ContextEnv reflects the override
	env := pm.ContextEnv()
	if env["BAR"] != "other" {
		t.Fatalf("expected env from overridden context, got: %#v", env)
	}
	if _, ok := env["FOO"]; ok {
		t.Fatalf("did not expect env from active context, got: %#v", env)
	}
}

func TestSetActiveContextOverrideDoesNotMutateActiveContext(t *testing.T) {
	pm := newTestPackageManager()
	if err := pm.SetActiveContextOverride("other"); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// The persisted active context must be unchanged
	if pm.state.ActiveContext != "default" {
		t.Fatalf(
			"override must not mutate persisted active context, got %q",
			pm.state.ActiveContext,
		)
	}
	name, _ := pm.ActiveContext()
	if name != "default" {
		t.Fatalf("ActiveContext must still return default, got %q", name)
	}
}

func TestSetActiveContextOverrideUnknownContext(t *testing.T) {
	pm := newTestPackageManager()
	err := pm.SetActiveContextOverride("nope")
	if !errors.Is(err, ErrContextNotExist) {
		t.Fatalf("expected ErrContextNotExist, got: %v", err)
	}
	// A failed override must leave targeting on the active context
	if pm.contextOverride != "" {
		t.Fatalf("failed override must not set contextOverride, got %q", pm.contextOverride)
	}
}
