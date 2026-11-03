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
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestRegistryPackagesFs(t *testing.T) {
	testRegistryDir := "test/registry/dir"
	testFs := fstest.MapFS{
		// This file is outside the registry dir and should not get processed
		"some/random/file.yaml": {},
		"test/registry/dir/packageA/packageA-1.2.3.yaml": {
			Data: []byte("name: packageA\nversion: 1.2.3"),
		},
		"test/registry/dir/packageA/packageA-2.3.4.yaml": {
			Data: []byte("name: packageA\nversion: 2.3.4"),
		},
		"test/registry/dir/packageB/packageB-3.4.5.yml": {
			Data: []byte("name: packageB\nversion: 3.4.5"),
		},
		// This file should get ignored without a YAML extension
		"test/registry/dir/some.file": {
			Data: []byte("name: packageC\nversion: 4.5.6"),
		},
	}
	testExpectedPkgs := []Package{
		{
			Name:    "packageA",
			Version: "1.2.3",
		},
		{
			Name:    "packageA",
			Version: "2.3.4",
		},
		{
			Name:    "packageB",
			Version: "3.4.5",
		},
	}
	cfg := Config{
		RegistryDir: testRegistryDir,
		Logger:      slog.Default(),
	}
	pkgs, err := registryPackagesFs(cfg, testFs, false)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// Remove filePath for easier comparison
	for idx, pkg := range pkgs {
		pkg.filePath = ""
		pkgs[idx] = pkg
	}
	if !reflect.DeepEqual(pkgs, testExpectedPkgs) {
		t.Fatalf(
			"did not get expected packages\n  got: %#v\n  expected: %#v",
			pkgs,
			testExpectedPkgs,
		)
	}
}

func TestRegistryPackagesRepositoryPriority(t *testing.T) {
	highPriority := t.TempDir()
	lowPriority := t.TempDir()
	writePackage := func(dir, name, version string) {
		t.Helper()
		path := filepath.Join(dir, name, name+"-"+version+".yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		contents := "name: " + name + "\nversion: " + version + "\n"
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePackage(highPriority, "overlay", "2.0.0")
	writePackage(highPriority, "overlay", "3.0.0")
	writePackage(lowPriority, "overlay", "1.0.0")
	writePackage(lowPriority, "base", "1.0.0")

	cfg := Config{
		CacheDir: t.TempDir(),
		Logger:   slog.Default(),
		Repositories: []Repository{
			{Dir: highPriority},
			{Dir: lowPriority},
		},
	}
	packages, err := registryPackages(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(packages) != 3 {
		t.Fatalf("got %d packages, expected 3: %#v", len(packages), packages)
	}
	if packages[0].Name != "overlay" || packages[1].Name != "overlay" {
		t.Fatalf("highest-priority repository did not retain all versions: %#v", packages)
	}
	if packages[2].Name != "base" {
		t.Fatalf("lower-priority package was not retained: %#v", packages[2])
	}
}
