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
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestInstallRejectsInvalidPackage checks that PackageManager.Install() runs
// package validation before performing any install steps. Packages loaded
// through AvailablePackages() are not validated (see loadPackageRegistry),
// so without this check an invalid package (here, one with a name containing
// characters disallowed by Package.validate()) would otherwise install
// successfully.
func TestInstallRejectsInvalidPackage(t *testing.T) {
	tmpDir := t.TempDir()
	pm := &PackageManager{
		config: Config{
			DataDir:  filepath.Join(tmpDir, "data"),
			CacheDir: filepath.Join(tmpDir, "cache"),
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			Template: NewTemplate(nil),
		},
		state: &State{
			ActiveContext: "default",
			Contexts: map[string]Context{
				"default": {Network: "preprod"},
			},
			PortRegistry: make(PortRegistry),
		},
	}
	pm.availablePackages = []Package{
		{
			Name:    "bad_pkg",
			Version: "1.0.0",
			InstallSteps: []PackageInstallStep{
				{
					File: &PackageInstallStepFile{
						Filename: "output",
						Content:  "hello",
					},
				},
			},
		},
	}

	if err := pm.Install("bad_pkg"); err == nil {
		t.Fatal("expected install to fail validation, got nil error")
	}
	if len(pm.state.InstalledPackages) != 0 {
		t.Fatalf(
			"expected no installed packages after failed validation, got: %#v",
			pm.state.InstalledPackages,
		)
	}
	writtenPath := filepath.Join(
		pm.config.DataDir,
		"bad_pkg-1.0.0-default",
		"output",
	)
	if _, err := os.Stat(writtenPath); !os.IsNotExist(err) {
		t.Fatalf(
			"expected no file written for a package that failed validation, got err: %v",
			err,
		)
	}
}

// TestUpgradeRejectsInvalidNewPackageVersion checks that PackageManager.
// Upgrade() validates the target package before deactivating and
// uninstalling the currently installed version. Without this check, an
// invalid upgrade target (here, a file install step missing content,
// source, and url) would still fail inside install(), but only after the
// working, installed version had already been torn down.
func TestUpgradeRejectsInvalidNewPackageVersion(t *testing.T) {
	tmpDir := t.TempDir()
	installedPkg := Package{
		Name:    "pkg",
		Version: "1.0.0",
	}
	pm := &PackageManager{
		config: Config{
			DataDir:  filepath.Join(tmpDir, "data"),
			CacheDir: filepath.Join(tmpDir, "cache"),
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			Template: NewTemplate(nil),
		},
		state: &State{
			ActiveContext: "default",
			Contexts: map[string]Context{
				"default": {Network: "preprod"},
			},
			InstalledPackages: []InstalledPackage{
				{
					Package: installedPkg,
					// InstalledPackage.IsEmpty() checks InstalledTime, not
					// the embedded Package fields, so this must be set for
					// the resolver to recognize this as already installed.
					InstalledTime: time.Now(),
					Context:       "default",
					Options:       map[string]bool{},
				},
			},
			PortRegistry: make(PortRegistry),
		},
	}
	pm.availablePackages = []Package{
		{
			Name:     "pkg",
			Version:  "2.0.0",
			filePath: filepath.Join("pkg", "pkg-2.0.0.yaml"),
			InstallSteps: []PackageInstallStep{
				{
					// Missing content/source/url; caught by
					// PackageInstallStepFile.validate().
					File: &PackageInstallStepFile{
						Filename: "output",
					},
				},
			},
		},
	}

	if err := pm.Upgrade("pkg"); err == nil {
		t.Fatal("expected upgrade to fail validation, got nil error")
	}
	if len(pm.state.InstalledPackages) != 1 ||
		pm.state.InstalledPackages[0].Package.Version != "1.0.0" {
		t.Fatalf(
			"expected the original version to remain installed, got: %#v",
			pm.state.InstalledPackages,
		)
	}
}

// TestInstallValidatesEntirePlanBeforeInstallingAny checks that Install()
// validates every package in the resolved plan before installing any of
// them. Without this, "app" depending on "dep" would install and persist
// "dep" (processed first in the resolved plan) before ever validating the
// invalid "app" itself, leaving a resolved dependency installed despite the
// overall command failing.
func TestInstallValidatesEntirePlanBeforeInstallingAny(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		DataDir:   filepath.Join(tmpDir, "data"),
		CacheDir:  filepath.Join(tmpDir, "cache"),
		ConfigDir: filepath.Join(tmpDir, "config"),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template:  NewTemplate(nil),
	}
	pm := &PackageManager{
		config: cfg,
		state: &State{
			config:        cfg,
			ActiveContext: "default",
			Contexts: map[string]Context{
				"default": {Network: "preprod"},
			},
			PortRegistry: make(PortRegistry),
		},
	}
	pm.availablePackages = []Package{
		{
			Name:     "dep",
			Version:  "1.0.0",
			filePath: filepath.Join("dep", "dep-1.0.0.yaml"),
			InstallSteps: []PackageInstallStep{
				{
					File: &PackageInstallStepFile{
						Filename: "output",
						Content:  "hello",
					},
				},
			},
		},
		{
			// invalid: missing filePath, so Package.validate rejects it
			Name:         "app",
			Version:      "1.0.0",
			Dependencies: []string{"dep"},
			InstallSteps: []PackageInstallStep{
				{
					File: &PackageInstallStepFile{
						Filename: "output",
						Content:  "hello",
					},
				},
			},
		},
	}

	if err := pm.Install("app"); err == nil {
		t.Fatal("expected install to fail validation, got nil error")
	}
	if len(pm.state.InstalledPackages) != 0 {
		t.Fatalf(
			"expected no installed packages (including resolved "+
				"dependencies) after failed validation, got: %#v",
			pm.state.InstalledPackages,
		)
	}
}

// TestUpgradeValidatesEntirePlanBeforeMutatingAny checks that Upgrade()
// validates every package in the resolved plan before deactivating,
// uninstalling, or installing any of them. Without this, upgrading "app"
// (processed first in the resolved plan) would tear down its old version
// and install the new one before ever validating the invalid new
// dependency "newdep" the new version pulls in.
func TestUpgradeValidatesEntirePlanBeforeMutatingAny(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		DataDir:   filepath.Join(tmpDir, "data"),
		CacheDir:  filepath.Join(tmpDir, "cache"),
		ConfigDir: filepath.Join(tmpDir, "config"),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template:  NewTemplate(nil),
	}
	installedApp := Package{
		Name:     "app",
		Version:  "1.0.0",
		filePath: filepath.Join("app", "app-1.0.0.yaml"),
	}
	pm := &PackageManager{
		config: cfg,
		state: &State{
			config:        cfg,
			ActiveContext: "default",
			Contexts: map[string]Context{
				"default": {Network: "preprod"},
			},
			InstalledPackages: []InstalledPackage{
				{
					Package:       installedApp,
					InstalledTime: time.Now(),
					Context:       "default",
					Options:       map[string]bool{},
				},
			},
			PortRegistry: make(PortRegistry),
		},
	}
	pm.availablePackages = []Package{
		{
			Name:         "app",
			Version:      "2.0.0",
			filePath:     filepath.Join("app", "app-2.0.0.yaml"),
			Dependencies: []string{"newdep"},
			InstallSteps: []PackageInstallStep{
				{
					File: &PackageInstallStepFile{
						Filename: "output",
						Content:  "hello",
					},
				},
			},
		},
		{
			// invalid: missing filePath, so Package.validate rejects it
			Name:    "newdep",
			Version: "1.0.0",
			InstallSteps: []PackageInstallStep{
				{
					File: &PackageInstallStepFile{
						Filename: "output",
						Content:  "hello",
					},
				},
			},
		},
	}

	if err := pm.Upgrade("app"); err == nil {
		t.Fatal("expected upgrade to fail validation, got nil error")
	}
	if len(pm.state.InstalledPackages) != 1 ||
		pm.state.InstalledPackages[0].Package.Version != "1.0.0" {
		t.Fatalf(
			"expected app to remain at its original version, got: %#v",
			pm.state.InstalledPackages,
		)
	}
}
