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
	pkgs, err := registryPackagesFs(cfg, testFs)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !reflect.DeepEqual(pkgs, testExpectedPkgs) {
		t.Fatalf("did not get expected packages\n  got: %#v\n  expected: %#v", pkgs, testExpectedPkgs)
	}
}
