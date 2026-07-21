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
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"text/template"
)

func newArchiveTestConfig(t *testing.T) Config {
	t.Helper()
	tmpDir := t.TempDir()
	return Config{
		CacheDir: tmpDir,
		DataDir:  tmpDir,
		BinDir:   filepath.Join(tmpDir, "bin"),
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
}

// TestPackageInstallStepFileValidate checks valid and invalid file-step archive
// configurations, including missing inputs, paths, and unsupported formats.
func TestPackageInstallStepFileValidate(t *testing.T) {
	testDefs := []struct {
		step      PackageInstallStepFile
		expectErr bool
	}{
		{
			step:      PackageInstallStepFile{Content: "foo"},
			expectErr: false,
		},
		{
			step:      PackageInstallStepFile{},
			expectErr: true,
		},
		{
			step: PackageInstallStepFile{
				Url:     "https://example.com/foo.zip",
				Archive: "zip",
			},
			expectErr: true, // missing archivePath
		},
		{
			step: PackageInstallStepFile{
				Url:     "https://example.com/foo.zip",
				Archive: "rar",
			},
			expectErr: true, // unsupported archive type
		},
		{
			step: PackageInstallStepFile{
				Url:         "https://example.com/foo.zip",
				Archive:     "zip",
				ArchivePath: "bin/foo",
			},
			expectErr: false,
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

// TestPackageInstallStepFileInstallArchiveZip checks that one binary is selected
// from a ZIP archive, installed with its content, and linked on activation.
func TestPackageInstallStepFileInstallArchiveZip(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	zipPath := filepath.Join(pkgSourceDir, "release.zip")
	zipData := buildTestZip(t, map[string]string{
		"bin/mybinary": testArchiveFileContent,
	})
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test zip: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.zip",
		Binary:      true,
		Mode:        0o755,
		Archive:     "zip",
		ArchivePath: "bin/mybinary",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "test-1.0.0-testctx", packagePath); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
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

	if err := step.activate(cfg, "test-1.0.0-testctx"); err != nil {
		t.Fatalf("activate failed: %s", err)
	}
	binPath := filepath.Join(cfg.BinDir, "mybinary")
	linkTarget, err := os.Readlink(binPath)
	if err != nil {
		t.Fatalf("unexpected error reading symlink: %s", err)
	}
	if linkTarget != writtenPath {
		t.Fatalf(
			"symlink target mismatch\n  got: %s\n  expected: %s",
			linkTarget,
			writtenPath,
		)
	}
}

// TestPackageInstallStepFileInstallArchiveTarGz checks that one binary can be
// selected from a tar.gz archive and written to the package data directory.
func TestPackageInstallStepFileInstallArchiveTarGz(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	tarGzPath := filepath.Join(pkgSourceDir, "release.tar.gz")
	tarGzData := buildTestTarGz(t, map[string]string{
		"mybinary": testArchiveFileContent,
	})
	if err := os.WriteFile(tarGzPath, tarGzData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test tar.gz: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.tar.gz",
		Binary:      true,
		Mode:        0o755,
		Archive:     "tar.gz",
		ArchivePath: "mybinary",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "test-1.0.0-testctx", packagePath); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
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

// TestPackageInstallStepFileInstallArchiveMissingEntry checks that installation
// fails when archivePath does not identify a file contained in the archive.
func TestPackageInstallStepFileInstallArchiveMissingEntry(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	zipPath := filepath.Join(pkgSourceDir, "release.zip")
	zipData := buildTestZip(t, map[string]string{
		"bin/other": testArchiveFileContent,
	})
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test zip: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.zip",
		Archive:     "zip",
		ArchivePath: "bin/mybinary",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "test-1.0.0-testctx", packagePath); err == nil {
		t.Fatal("expected error for missing archive entry, got nil")
	}
}

// TestPackageInstallStepFileInstallUrlArchive checks downloading a templated
// tar.gz URL, extracting its binary, and linking the binary on activation.
func TestPackageInstallStepFileInstallUrlArchive(t *testing.T) {
	tarGzData := buildTestTarGz(t, map[string]string{
		"mybinary": testArchiveFileContent,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/mybinary-linux-amd64.tar.gz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(tarGzData) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := newArchiveTestConfig(t)
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"System": map[string]string{
				"OS":   "linux",
				"ARCH": "amd64",
			},
		},
	)

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Binary:      true,
		Mode:        0o755,
		Url:         srv.URL + "/releases/mybinary-{{ .System.OS }}-{{ .System.ARCH }}.tar.gz",
		Archive:     "tar.gz",
		ArchivePath: "mybinary",
	}
	if err := step.install(cfg, "test-1.0.0-testctx", ""); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
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

	if err := step.activate(cfg, "test-1.0.0-testctx"); err != nil {
		t.Fatalf("activate failed: %s", err)
	}
	binPath := filepath.Join(cfg.BinDir, "mybinary")
	linkTarget, err := os.Readlink(binPath)
	if err != nil {
		t.Fatalf("unexpected error reading symlink: %s", err)
	}
	if linkTarget != writtenPath {
		t.Fatalf(
			"symlink target mismatch\n  got: %s\n  expected: %s",
			linkTarget,
			writtenPath,
		)
	}
}

// TestPackageInstallStepFileInstallArchivePathTemplated checks that OS and
// architecture values are rendered when selecting a file inside an archive.
func TestPackageInstallStepFileInstallArchivePathTemplated(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"System": map[string]string{
				"OS":   "linux",
				"ARCH": "amd64",
			},
		},
	)
	pkgSourceDir := t.TempDir()
	zipPath := filepath.Join(pkgSourceDir, "release.zip")
	zipData := buildTestZip(t, map[string]string{
		"bin/linux-amd64/mybinary": testArchiveFileContent,
	})
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test zip: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.zip",
		Archive:     "zip",
		ArchivePath: "bin/{{ .System.OS }}-{{ .System.ARCH }}/mybinary",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "test-1.0.0-testctx", packagePath); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
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

// TestPackageInstallStepFileInstallUrlTemplated checks that OS and architecture
// template values are correctly substituted into a binary download URL.
func TestPackageInstallStepFileInstallUrlTemplated(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"System": map[string]string{
				"OS":   "linux",
				"ARCH": "amd64",
			},
		},
	)
	// Render the URL template directly to confirm OS/ARCH substitution works
	// as expected for the file install step's url field
	rendered, err := cfg.Template.Render(
		"https://example.com/mybinary-{{ .System.OS }}-{{ .System.ARCH }}",
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error rendering url template: %s", err)
	}
	expected := "https://example.com/mybinary-linux-amd64"
	if rendered != expected {
		t.Fatalf(
			"did not get expected rendered url\n  got: %s\n  expected: %s",
			rendered,
			expected,
		)
	}
}

// TestPackageInstallStepFileInstallArchiveModePreserved checks that the requested
// file permissions are applied to the binary extracted from an archive.
func TestPackageInstallStepFileInstallArchiveModePreserved(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	zipPath := filepath.Join(pkgSourceDir, "release.zip")
	zipData := buildTestZip(t, map[string]string{
		"mybinary": testArchiveFileContent,
	})
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test zip: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.zip",
		Mode:        0o755,
		Archive:     "zip",
		ArchivePath: "mybinary",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "test-1.0.0-testctx", packagePath); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
	stat, err := os.Stat(writtenPath)
	if err != nil {
		t.Fatalf("unexpected error statting installed file: %s", err)
	}
	if stat.Mode().Perm() != fs.FileMode(0o755) {
		t.Fatalf(
			"unexpected file mode: got %o, expected %o",
			stat.Mode().Perm(),
			0o755,
		)
	}
}
