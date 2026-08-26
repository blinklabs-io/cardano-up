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
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"
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
		{
			step: PackageInstallStepFile{
				Content:     "foo",
				Archive:     "zip",
				ArchivePath: "bin/foo",
			},
			expectErr: true, // archive cannot be combined with content
		},
		{
			step: PackageInstallStepFile{
				Url:            "https://example.com/foo.zip",
				Archive:        "zip",
				ArchivePath:    "bin/foo",
				ArchiveMaxSize: 1024,
			},
			expectErr: false,
		},
		{
			step: PackageInstallStepFile{
				Content:        "foo",
				ArchiveMaxSize: 1024,
			},
			expectErr: true, // archiveMaxSize requires archive
		},
		{
			step: PackageInstallStepFile{
				Url:            "https://example.com/foo.zip",
				Archive:        "zip",
				ArchivePath:    "bin/foo",
				ArchiveMaxSize: -1,
			},
			expectErr: true, // archiveMaxSize cannot be negative
		},
		{
			step: PackageInstallStepFile{
				Url:            "https://example.com/foo.zip",
				Archive:        "zip",
				ArchivePath:    "bin/foo",
				ArchiveMaxSize: maxArchiveSizeCeiling + 1,
			},
			expectErr: true, // archiveMaxSize exceeds ceiling
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

// TestPackageInstallStepFileInstallArchiveZip checks that one binary is
// selected from a ZIP archive, installed with its content, and linked on
// activation.
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

// TestPackageInstallStepFileActivateTemplatedFilename checks that activate()
// symlinks to the same rendered path install() wrote the file at, when
// filename contains a template expression. Regression test for a bug where
// activate() built the symlink source from the raw, un-rendered filename
// while the symlink destination used the rendered one, producing a dangling
// symlink for any package using a per-OS/ARCH templated filename.
func TestPackageInstallStepFileActivateTemplatedFilename(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"System": map[string]string{
				"OS": "linux",
			},
		},
	)
	step := &PackageInstallStepFile{
		Filename: "{{ .System.OS }}-mybinary",
		Content:  testArchiveFileContent,
		Binary:   true,
		Mode:     0o755,
	}
	if err := step.install(cfg, "test-1.0.0-testctx", ""); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	writtenPath := filepath.Join(
		cfg.DataDir,
		"test-1.0.0-testctx",
		"linux-mybinary",
	)
	if _, err := os.Stat(writtenPath); err != nil {
		t.Fatalf("expected installed file at %s: %s", writtenPath, err)
	}

	if err := step.activate(cfg, "test-1.0.0-testctx"); err != nil {
		t.Fatalf("activate failed: %s", err)
	}
	binPath := filepath.Join(cfg.BinDir, "linux-mybinary")
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

// TestPackageInstallStepFileInstallArchiveTgz checks that the tgz alias works
// end to end when extracting and installing one binary from an archive.
func TestPackageInstallStepFileInstallArchiveTgz(t *testing.T) {
	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	tgzPath := filepath.Join(pkgSourceDir, "release.tgz")
	tgzData := buildTestTarGz(t, map[string]string{
		"mybinary": testArchiveFileContent,
	})
	if err := os.WriteFile(tgzPath, tgzData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test tgz: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.tgz",
		Binary:      true,
		Mode:        0o755,
		Archive:     "tgz",
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
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/releases/mybinary-linux-amd64.tar.gz" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(tarGzData) //nolint:errcheck
		}),
	)
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
		Filename: "mybinary",
		Binary:   true,
		Mode:     0o755,
		Url: srv.URL +
			"/releases/mybinary-{{ .System.OS }}-{{ .System.ARCH }}.tar.gz",
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

// TestPackageInstallStepFileInstallUrlDownloadSizeLimit checks that install
// aborts once a url-sourced download exceeds maxDownloadSize, instead of
// buffering the full oversized response into memory.
func TestPackageInstallStepFileInstallUrlDownloadSizeLimit(t *testing.T) {
	origMaxDownloadSize := maxDownloadSize
	maxDownloadSize = 10
	t.Cleanup(func() {
		maxDownloadSize = origMaxDownloadSize
	})

	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(strings.Repeat("x", 100))) //nolint:errcheck
		}),
	)
	defer srv.Close()

	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepFile{
		Filename: "mybinary",
		Url:      srv.URL + "/mybinary",
	}
	err := step.install(cfg, "test-1.0.0-testctx", "")
	if err == nil {
		t.Fatal("expected error for oversized download, got nil")
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
	if _, statErr := os.Stat(writtenPath); statErr == nil {
		t.Fatal("expected no file to be written for oversized download")
	}
}

// TestPackageInstallStepFileInstallUrlDownloadTimeout checks that install
// aborts a url-sourced download once it exceeds downloadTimeout, instead of
// hanging indefinitely against a slow or stalling server. install() is run
// in a goroutine under a bounded watchdog: without the production timeout
// in place, the test server's handler blocks forever (it's only released
// by closeBlock, which a broken fix would never let this test reach), so a
// direct, synchronous call would hang until the whole test binary's global
// timeout instead of failing this test with a clear message.
func TestPackageInstallStepFileInstallUrlDownloadTimeout(t *testing.T) {
	origDownloadTimeout := downloadTimeout
	downloadTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		downloadTimeout = origDownloadTimeout
	})

	blockCh := make(chan struct{})
	var closeBlockOnce sync.Once
	closeBlock := func() { closeBlockOnce.Do(func() { close(blockCh) }) }
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-blockCh
		}),
	)
	defer func() {
		closeBlock()
		srv.Close()
	}()

	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepFile{
		Filename: "mybinary",
		Url:      srv.URL + "/mybinary",
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- step.install(cfg, "test-1.0.0-testctx", "")
	}()

	var err error
	select {
	case err = <-errCh:
	case <-time.After(5 * time.Second):
		closeBlock()
		t.Fatal(
			"install() did not return within the watchdog window; " +
				"the production download timeout does not appear to be enforced",
		)
	}
	if err == nil {
		t.Fatal("expected error for a download exceeding the timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a context.DeadlineExceeded error, got: %v", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
	if _, statErr := os.Stat(writtenPath); statErr == nil {
		t.Fatal("expected no file to be written for a timed-out download")
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

// TestPackageInstallStepFileInstallUrlTemplated checks that install renders
// OS and architecture values into the url field before issuing the HTTP
// request, so the server actually receives the platform-specific path.
func TestPackageInstallStepFileInstallUrlTemplated(t *testing.T) {
	const expectedContent = "binary content"
	var requestedPath string
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			w.Write([]byte(expectedContent)) //nolint:errcheck
		}),
	)
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
		Filename: "mybinary",
		Url:      srv.URL + "/mybinary-{{ .System.OS }}-{{ .System.ARCH }}",
	}
	if err := step.install(cfg, "test-1.0.0-testctx", ""); err != nil {
		t.Fatalf("install failed: %s", err)
	}

	expectedPath := "/mybinary-linux-amd64"
	if requestedPath != expectedPath {
		t.Fatalf(
			"did not get expected requested path\n  got: %s\n  expected: %s",
			requestedPath,
			expectedPath,
		)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
	content, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("unexpected error reading installed file: %s", err)
	}
	if string(content) != expectedContent {
		t.Fatalf(
			"did not get expected content\n  got: %q\n  expected: %q",
			content,
			expectedContent,
		)
	}
}

// TestPackageInstallStepFileInstallSourceArchiveSizeLimit checks that install
// aborts once a source-backed archive file exceeds maxDownloadSize, instead
// of buffering the full oversized file into memory.
func TestPackageInstallStepFileInstallSourceArchiveSizeLimit(t *testing.T) {
	origMaxDownloadSize := maxDownloadSize
	maxDownloadSize = 10
	t.Cleanup(func() {
		maxDownloadSize = origMaxDownloadSize
	})

	pkgSourceDir := t.TempDir()
	zipPath := filepath.Join(pkgSourceDir, "release.zip")
	zipData := buildTestZip(t, map[string]string{
		"mybinary": strings.Repeat("x", 100),
	})
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test zip: %s", err)
	}

	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.zip",
		Archive:     "zip",
		ArchivePath: "mybinary",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	err := step.install(cfg, "test-1.0.0-testctx", packagePath)
	if err == nil {
		t.Fatal("expected error for oversized source archive, got nil")
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
	if _, statErr := os.Stat(writtenPath); statErr == nil {
		t.Fatal("expected no file to be written for oversized source archive")
	}
}

// TestPackageInstallStepFileInstallArchiveMaxSizeOverride checks that a
// package-level archiveMaxSize is actually applied to extraction, by setting
// it below the size of an entry that would otherwise pass under the default
// maxArchiveEntrySize.
func TestPackageInstallStepFileInstallArchiveMaxSizeOverride(t *testing.T) {
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
		Filename:       "mybinary",
		Source:         "release.tar.gz",
		Archive:        "tar.gz",
		ArchivePath:    "mybinary",
		ArchiveMaxSize: int64(len(testArchiveFileContent)) - 1,
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	err := step.install(cfg, "test-1.0.0-testctx", packagePath)
	if err == nil {
		t.Fatal("expected error from archiveMaxSize override, got nil")
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
	if _, statErr := os.Stat(writtenPath); statErr == nil {
		t.Fatal(
			"expected no file to be written when archiveMaxSize is exceeded",
		)
	}
}

// TestPackageInstallStepFileInstallArchiveMaxSizeAllowsLargerEntry checks
// that raising archiveMaxSize permits extracting an entry that exceeds the
// default maxArchiveEntrySize, proving the override actually reaches the
// extraction path rather than the default silently still applying. It lowers
// maxArchiveEntrySize itself so the "large" entry can stay tiny.
func TestPackageInstallStepFileInstallArchiveMaxSizeAllowsLargerEntry(
	t *testing.T,
) {
	origMaxArchiveEntrySize := maxArchiveEntrySize
	maxArchiveEntrySize = 10
	t.Cleanup(func() {
		maxArchiveEntrySize = origMaxArchiveEntrySize
	})

	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	tarGzPath := filepath.Join(pkgSourceDir, "release.tar.gz")
	tarGzData := buildTestTarGz(t, map[string]string{
		"mybinary": testArchiveFileContent, // 21 bytes, exceeds the lowered default
	})
	if err := os.WriteFile(tarGzPath, tarGzData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test tar.gz: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:       "mybinary",
		Source:         "release.tar.gz",
		Archive:        "tar.gz",
		ArchivePath:    "mybinary",
		ArchiveMaxSize: 4096,
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "test-1.0.0-testctx", packagePath); err != nil {
		t.Fatalf("install failed despite archiveMaxSize override: %s", err)
	}

	writtenPath := filepath.Join(cfg.DataDir, "test-1.0.0-testctx", "mybinary")
	content, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("unexpected error reading installed file: %s", err)
	}
	if string(content) != testArchiveFileContent {
		t.Fatal("installed content did not match the archive entry")
	}
}

// TestPackageInstallStepFileInstallArchiveMaxSizeDefaultStillApplies checks
// that without an override, an entry exceeding the (lowered, for the test)
// default maxArchiveEntrySize is still rejected, so the prior test's success
// is actually due to the override and not a bypassed check.
func TestPackageInstallStepFileInstallArchiveMaxSizeDefaultStillApplies(
	t *testing.T,
) {
	origMaxArchiveEntrySize := maxArchiveEntrySize
	maxArchiveEntrySize = 10
	t.Cleanup(func() {
		maxArchiveEntrySize = origMaxArchiveEntrySize
	})

	cfg := newArchiveTestConfig(t)
	pkgSourceDir := t.TempDir()
	tarGzPath := filepath.Join(pkgSourceDir, "release.tar.gz")
	tarGzData := buildTestTarGz(t, map[string]string{
		"mybinary": testArchiveFileContent, // 21 bytes, exceeds the lowered default
	})
	if err := os.WriteFile(tarGzPath, tarGzData, 0o644); err != nil {
		t.Fatalf("unexpected error writing test tar.gz: %s", err)
	}

	step := &PackageInstallStepFile{
		Filename:    "mybinary",
		Source:      "release.tar.gz",
		Archive:     "tar.gz",
		ArchivePath: "mybinary",
	}
	packagePath := filepath.Join(pkgSourceDir, "test-1.0.0.yaml")
	if err := step.install(cfg, "test-1.0.0-testctx", packagePath); err == nil {
		t.Fatal("expected error without archiveMaxSize override, got nil")
	}
}

// TestPackageInstallStepFileInstallArchiveModePreserved checks that the
// requested file permissions are applied to the binary extracted from an
// archive.
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
