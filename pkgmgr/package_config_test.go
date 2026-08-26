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
	"os"
	"path/filepath"
	"testing"
)

// TestPackageInstallStepConfigValidate checks that the config install step
// enforces the same content-source rules as the file install step.
func TestPackageInstallStepConfigValidate(t *testing.T) {
	testDefs := []struct {
		step      PackageInstallStepConfig
		expectErr bool
	}{
		{
			step:      PackageInstallStepConfig{Filename: "settings.yaml", Content: "foo"},
			expectErr: false,
		},
		{
			step:      PackageInstallStepConfig{},
			expectErr: true,
		},
		{
			step:      PackageInstallStepConfig{Content: "foo"},
			expectErr: true, // missing filename
		},
		{
			step: PackageInstallStepConfig{
				Filename: "settings.yaml",
				Url:      "https://example.com/foo.zip",
				Archive:  "zip",
			},
			expectErr: true, // missing archivePath
		},
		{
			step: PackageInstallStepConfig{
				Filename:    "settings.yaml",
				Content:     "foo",
				Archive:     "zip",
				ArchivePath: "bin/foo",
			},
			expectErr: true, // archive cannot be combined with content
		},
	}
	cfg := newArchiveTestConfig(t)
	for _, testDef := range testDefs {
		err := testDef.step.validate(cfg)
		if testDef.expectErr && err == nil {
			t.Errorf("expected error for step %#v, got nil", testDef.step)
		}
		if !testDef.expectErr && err != nil {
			t.Errorf("unexpected error for step %#v: %s", testDef.step, err)
		}
	}
}

// TestPackageInstallStepConfigInstallWritesOnFirstInstall checks that a
// config file is written under the context directory (not the per-version
// package data directory) the first time it is installed.
func TestPackageInstallStepConfigInstallWritesOnFirstInstall(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepConfig{
		Filename: "myapp/settings.yaml",
		Content:  "default: true\n",
	}
	if err := step.install(cfg, "testctx", ""); err != nil {
		t.Fatalf("install failed: %s", err)
	}
	writtenPath := filepath.Join(cfg.DataDir, "testctx", "myapp/settings.yaml")
	content, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("unexpected error reading installed file: %s", err)
	}
	if string(content) != "default: true\n" {
		t.Fatalf(
			"did not get expected content\n  got: %q\n  expected: %q",
			content,
			"default: true\n",
		)
	}
}

// TestPackageInstallStepConfigInstallPreservesExistingFile checks that a
// second install (as happens on package upgrade) does not overwrite a config
// file that already exists, even though its rendered content differs. This
// is the behavior requested in issue #567: a config file survives a package
// upgrade with any user edits intact.
func TestPackageInstallStepConfigInstallPreservesExistingFile(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepConfig{
		Filename: "settings.yaml",
		Content:  "default: true\n",
	}
	if err := step.install(cfg, "testctx", ""); err != nil {
		t.Fatalf("first install failed: %s", err)
	}
	writtenPath := filepath.Join(cfg.DataDir, "testctx", "settings.yaml")
	userContent := "default: true\ncustom: user edit\n"
	if err := os.WriteFile(writtenPath, []byte(userContent), 0o644); err != nil {
		t.Fatalf("failed to simulate user edit: %s", err)
	}

	upgradeStep := &PackageInstallStepConfig{
		Filename: "settings.yaml",
		Content:  "default: true\nnewOption: added-in-upgrade\n",
	}
	if err := upgradeStep.install(cfg, "testctx", ""); err != nil {
		t.Fatalf("second install failed: %s", err)
	}

	content, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("unexpected error reading installed file: %s", err)
	}
	if string(content) != userContent {
		t.Fatalf(
			"install overwrote existing config file\n  got: %q\n  expected: %q",
			content,
			userContent,
		)
	}
}

// TestPackageInstallStepConfigInstallPathTraversal checks that a filename
// escaping the context directory is rejected, mirroring the file install
// step's path traversal check.
func TestPackageInstallStepConfigInstallPathTraversal(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepConfig{
		Filename: "../escape.yaml",
		Content:  "foo",
	}
	if err := step.install(cfg, "testctx", ""); err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "escape.yaml")); err == nil {
		t.Fatal("expected no file to be written outside the context directory")
	}
}

// TestPackageInstallStepConfigInstallSymlinkedParentRejected checks that a
// symlink placed inside the context directory cannot be used to redirect a
// write outside of it, even though the filename itself contains no "..".
func TestPackageInstallStepConfigInstallSymlinkedParentRejected(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	contextDir := filepath.Join(cfg.DataDir, "testctx")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatalf("unexpected error creating context directory: %s", err)
	}
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(contextDir, "link")); err != nil {
		t.Fatalf("unexpected error creating symlink: %s", err)
	}

	step := &PackageInstallStepConfig{
		Filename: "link/settings.yaml",
		Content:  "foo",
	}
	if err := step.install(cfg, "testctx", ""); err == nil {
		t.Fatal("expected error for symlink escape, got nil")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "settings.yaml")); err == nil {
		t.Fatal("expected no file to be written outside the context directory via the symlink")
	}
}

// TestPackageInstallStepConfigInstallFromArchive checks that config content
// can be extracted from a source archive, reusing the same resolution logic
// as the file install step.
func TestPackageInstallStepConfigInstallFromArchive(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	zipPath := filepath.Join(pkgSourceDir, "release.zip")
	zipData := buildTestZip(t, map[string]string{
		"config/settings.yaml": testArchiveFileContent,
	})
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test zip: %s", err)
	}

	step := &PackageInstallStepConfig{
		Filename:    "settings.yaml",
		Source:      "release.zip",
		Archive:     "zip",
		ArchivePath: "config/settings.yaml",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "testctx", packagePath); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "testctx", "settings.yaml")
	content, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("unexpected error reading installed file: %s", err)
	}
	if string(content) != testArchiveFileContent {
		t.Fatalf(
			"did not get expected content\n  got: %q\n  expected: %q",
			content,
			testArchiveFileContent,
		)
	}
}

// TestPackageInstallStepConfigUninstallPreservesFile checks that uninstall
// leaves the config file in place instead of deleting it.
func TestPackageInstallStepConfigUninstallPreservesFile(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepConfig{
		Filename: "settings.yaml",
		Content:  "default: true\n",
	}
	if err := step.install(cfg, "testctx", ""); err != nil {
		t.Fatalf("install failed: %s", err)
	}
	if err := step.uninstall(cfg, "testctx"); err != nil {
		t.Fatalf("uninstall failed: %s", err)
	}
	writtenPath := filepath.Join(cfg.DataDir, "testctx", "settings.yaml")
	if _, err := os.Stat(writtenPath); err != nil {
		t.Fatalf("expected config file to survive uninstall: %s", err)
	}
}

// TestPackageUpgradePreservesConfigFileAcrossVersions exercises the same
// deactivate/uninstall(keepData=true)/install sequence PackageManager.Upgrade
// runs, across two package versions sharing a context, and checks that a
// user edit made to the config file after the first install survives the
// upgrade to the second version untouched.
func TestPackageUpgradePreservesConfigFileAcrossVersions(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	const context = "testctx"

	newPkg := func(version, content string) Package {
		return Package{
			Name:    "myapp",
			Version: version,
			InstallSteps: []PackageInstallStep{
				{
					Config: &PackageInstallStepConfig{
						Filename: "config/settings.yaml",
						Content:  content,
					},
				},
			},
		}
	}
	v1 := newPkg("1.0.0", "version: 1.0.0 default\n")
	v2 := newPkg("2.0.0", "version: 2.0.0 default\n")

	if _, _, _, err := v1.install(cfg, context, nil, false, nil); err != nil {
		t.Fatalf("v1 install failed: %s", err)
	}

	configPath := filepath.Join(cfg.DataDir, context, "config", "settings.yaml")
	userContent := "version: 1.0.0 default\ncustom: user edit\n"
	if err := os.WriteFile(configPath, []byte(userContent), 0o644); err != nil {
		t.Fatalf("failed to simulate user edit: %s", err)
	}

	// Mirrors PackageManager.Upgrade: uninstall the old version with
	// keepData=true, then install the new version into the same context.
	if err := v1.uninstall(cfg, context, true, false); err != nil {
		t.Fatalf("v1 uninstall failed: %s", err)
	}
	if _, _, _, err := v2.install(cfg, context, nil, false, nil); err != nil {
		t.Fatalf("v2 install failed: %s", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file after upgrade: %s", err)
	}
	if string(got) != userContent {
		t.Fatalf(
			"upgrade clobbered user config\n  got: %q\n  expected: %q",
			got,
			userContent,
		)
	}
}

// TestPackageInstallStepMultipleMethodsRejected checks that specifying more
// than one install method (docker, file, config) on a single step is
// rejected, now that there are three mutually exclusive method fields.
func TestPackageInstallStepMultipleMethodsRejected(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	pkg := Package{
		Name:    "myapp",
		Version: "1.0.0",
		InstallSteps: []PackageInstallStep{
			{
				File:   &PackageInstallStepFile{Content: "foo"},
				Config: &PackageInstallStepConfig{Content: "foo"},
			},
		},
	}
	if _, _, _, err := pkg.install(cfg, "testctx", nil, false, nil); err == nil {
		t.Fatal("expected error for multiple install methods, got nil")
	}
}
