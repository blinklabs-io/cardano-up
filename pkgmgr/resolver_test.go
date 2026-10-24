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
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSplitPackage(t *testing.T) {
	testDefs := []struct {
		Package     string
		Name        string
		VersionSpec string
		Options     map[string]bool
	}{
		{
			Package:     "test-packageB[foo,-bar] >= 1.2.3",
			Name:        "test-packageB",
			VersionSpec: ">= 1.2.3",
			Options: map[string]bool{
				"foo": true,
				"bar": false,
			},
		},
		{
			Package:     "test-package<1.2.4",
			Name:        "test-package",
			VersionSpec: "<1.2.4",
		},
		{
			Package: "test-package",
			Name:    "test-package",
		},
		{
			Package: "test-package[foo",
			Name:    "test-package[foo",
		},
	}
	for _, testDef := range testDefs {
		tmpResolver := &Resolver{}
		pkgName, pkgVersionSpec, pkgOpts := tmpResolver.splitPackage(
			testDef.Package,
		)
		if pkgName != testDef.Name {
			t.Fatalf(
				"did not get expected package name: got %q, expected %q",
				pkgName,
				testDef.Name,
			)
		}
		if pkgVersionSpec != testDef.VersionSpec {
			t.Fatalf(
				"did not get expected package version spec: got %q, expected %q",
				pkgVersionSpec,
				testDef.VersionSpec,
			)
		}
		if len(pkgOpts) > 0 && len(testDef.Options) > 0 {
			if !reflect.DeepEqual(pkgOpts, testDef.Options) {
				t.Fatalf(
					"did not get expected package options\n  got: %#v\n  expected: %#v",
					pkgOpts,
					testDef.Options,
				)
			}
		}
	}
}

func TestPackageDependenciesYAML(t *testing.T) {
	manifest := `name: app
version: 1.0.0
dependencies:
  - config >= 1.0.0
  - - node >= 2.0.0
    - dolos >= 0.30.0
`
	pkg, err := NewPackageFromReader(strings.NewReader(manifest))
	if err != nil {
		t.Fatalf("failed to decode package: %v", err)
	}
	want := []PackageDependency{
		{"config >= 1.0.0"},
		{"node >= 2.0.0", "dolos >= 0.30.0"},
	}
	if !reflect.DeepEqual(pkg.Dependencies, want) {
		t.Fatalf(
			"unexpected dependencies: got %#v, want %#v",
			pkg.Dependencies,
			want,
		)
	}

	encoded, err := yaml.Marshal(pkg)
	if err != nil {
		t.Fatalf("failed to encode package: %v", err)
	}
	if !strings.Contains(string(encoded), "- config >= 1.0.0") {
		t.Fatalf("single dependency did not retain scalar form:\n%s", encoded)
	}
}

func TestPackageDependencyValidation(t *testing.T) {
	tests := []struct {
		name       string
		dependency PackageDependency
	}{
		{name: "empty alternatives", dependency: PackageDependency{}},
		{name: "empty provider", dependency: PackageDependency{""}},
		{
			name:       "duplicate provider",
			dependency: PackageDependency{"node >= 1.0.0", "node >= 2.0.0"},
		},
		{
			name:       "invalid constraint",
			dependency: PackageDependency{"node not-a-version"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := Package{
				Name:         "app",
				Version:      "1.0.0",
				Dependencies: []PackageDependency{test.dependency},
				filePath:     "app/app-1.0.0.yaml",
			}
			if err := pkg.validate(Config{}); err == nil {
				t.Fatal("expected invalid dependency to be rejected")
			}
		})
	}
}

func TestResolverInstallAlternativeDependencyDefault(t *testing.T) {
	app := Package{
		Name:         "app",
		Version:      "1.0.0",
		Dependencies: []PackageDependency{{"node >= 2.0.0", "dolos >= 0.30.0"}},
	}
	resolver := newTestResolver(t, nil, []Package{
		app,
		{Name: "node", Version: "2.0.0"},
		{Name: "dolos", Version: "0.30.0"},
	})
	plan, err := resolver.Install("app")
	if err != nil {
		t.Fatalf("failed to resolve install: %v", err)
	}
	if len(plan) != 2 || plan[0].Install.Name != "node" ||
		plan[1].Install.Name != "app" {
		t.Fatalf("unexpected install plan: %#v", plan)
	}
}

func TestResolverInstallFallsBackToAvailableAlternative(t *testing.T) {
	app := Package{
		Name:         "app",
		Version:      "1.0.0",
		Dependencies: []PackageDependency{{"node >= 2.0.0", "dolos >= 0.30.0"}},
	}
	resolver := newTestResolver(t, nil, []Package{
		app,
		{Name: "dolos", Version: "0.30.0"},
	})
	plan, err := resolver.Install("app")
	if err != nil {
		t.Fatalf("failed to resolve install: %v", err)
	}
	if len(plan) != 2 || plan[0].Install.Name != "dolos" {
		t.Fatalf("unexpected install plan: %#v", plan)
	}
}

func TestResolverInstallUsesInstalledAlternative(t *testing.T) {
	app := Package{
		Name:         "app",
		Version:      "1.0.0",
		Dependencies: []PackageDependency{{"node >= 2.0.0", "dolos >= 0.30.0"}},
	}
	installed := []InstalledPackage{
		newTestInstalled(Package{Name: "dolos", Version: "0.30.0"}),
	}
	resolver := newTestResolver(t, installed, []Package{
		app,
		{Name: "node", Version: "2.0.0"},
	})
	plan, err := resolver.Install("app")
	if err != nil {
		t.Fatalf("failed to resolve install: %v", err)
	}
	if len(plan) != 1 || plan[0].Install.Name != "app" {
		t.Fatalf("unexpected install plan: %#v", plan)
	}
}

func TestResolverInstallUnsatisfiedAlternatives(t *testing.T) {
	app := Package{
		Name:         "app",
		Version:      "1.0.0",
		Dependencies: []PackageDependency{{"node >= 2.0.0", "dolos >= 0.30.0"}},
	}
	resolver := newTestResolver(t, nil, []Package{app})
	_, err := resolver.Install("app")
	if err == nil ||
		!strings.Contains(err.Error(), "node >= 2.0.0 or dolos >= 0.30.0") {
		t.Fatalf("expected alternatives in resolution error, got %v", err)
	}
}

func TestResolverInstallDoesNotDuplicateUnsatisfiedProviders(t *testing.T) {
	app := Package{
		Name:         "app",
		Version:      "1.0.0",
		Dependencies: []PackageDependency{{"node >= 2.0.0", "dolos >= 0.30.0"}},
	}
	installed := []InstalledPackage{
		newTestInstalled(Package{Name: "node", Version: "1.0.0"}),
		newTestInstalled(Package{Name: "dolos", Version: "0.29.0"}),
	}
	resolver := newTestResolver(t, installed, []Package{
		app,
		{Name: "node", Version: "2.0.0"},
		{Name: "dolos", Version: "0.30.0"},
	})
	if _, err := resolver.Install("app"); err == nil {
		t.Fatal(
			"install should not add another version of an installed provider",
		)
	}
}

func TestResolverUninstallAlternativeDependencySafety(t *testing.T) {
	dependency := PackageDependency{"node >= 2.0.0", "dolos >= 0.30.0"}
	app := newTestInstalled(
		Package{
			Name:         "app",
			Version:      "1.0.0",
			Dependencies: []PackageDependency{dependency},
		},
	)
	node := newTestInstalled(Package{Name: "node", Version: "2.0.0"})
	dolos := newTestInstalled(Package{Name: "dolos", Version: "0.30.0"})

	withAlternative := newTestResolver(
		t,
		[]InstalledPackage{app, node, dolos},
		nil,
	)
	if err := withAlternative.Uninstall(node); err != nil {
		t.Fatalf(
			"uninstall should preserve dependency through alternative: %v",
			err,
		)
	}

	withoutAlternative := newTestResolver(t, []InstalledPackage{app, node}, nil)
	if err := withoutAlternative.Uninstall(node); err == nil {
		t.Fatal(
			"uninstall should reject removal of the only satisfying provider",
		)
	}
}

func TestResolverUpgradeAlternativeDependencyConstraints(t *testing.T) {
	dependency := PackageDependency{"node < 3.0.0", "dolos >= 0.30.0"}
	app := newTestInstalled(
		Package{
			Name:         "app",
			Version:      "1.0.0",
			Dependencies: []PackageDependency{dependency},
		},
	)
	node := newTestInstalled(Package{Name: "node", Version: "2.0.0"})
	node3 := Package{Name: "node", Version: "3.0.0"}

	soleProvider := newTestResolver(
		t,
		[]InstalledPackage{app, node},
		[]Package{node3},
	)
	if _, err := soleProvider.Upgrade("node"); err == nil {
		t.Fatal(
			"upgrade should retain the only provider's dependency constraint",
		)
	}

	dolos := newTestInstalled(Package{Name: "dolos", Version: "0.30.0"})
	withAlternative := newTestResolver(
		t,
		[]InstalledPackage{app, node, dolos},
		[]Package{node3},
	)
	plan, err := withAlternative.Upgrade("node")
	if err != nil {
		t.Fatalf(
			"upgrade should be allowed while another provider satisfies the dependency: %v",
			err,
		)
	}
	if len(plan) != 1 || plan[0].Upgrade.Version != "3.0.0" {
		t.Fatalf("unexpected upgrade plan: %#v", plan)
	}
}

func newTestResolver(
	t *testing.T,
	installed []InstalledPackage,
	available []Package,
) *Resolver {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	resolver, err := NewResolver(installed, available, "test", logger)
	if err != nil {
		t.Fatalf("failed to create resolver: %v", err)
	}
	return resolver
}

func newTestInstalled(pkg Package) InstalledPackage {
	return NewInstalledPackage(pkg, "test", "", nil, nil)
}
