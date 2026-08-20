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
	"strings"
	"testing"
)

// writeHookScript writes a /bin/sh script that appends label to hookLog and
// returns its path. runHookScript renders the script value and runs it with
// `/bin/sh -c`, so a path is a valid hook body.
func writeHookScript(t *testing.T, dir, name, hookLog, label, extra string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\n" + extra + "echo '" + label + "' >> " + hookLog + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
	return path
}

func readHookLog(t *testing.T, hookLog string) []string {
	t.Helper()
	contents, err := os.ReadFile(hookLog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read hook log: %v", err)
	}
	return strings.Fields(strings.TrimSpace(string(contents)))
}

// TestInstall_RunsPreStartHookAfterInstallSteps verifies that installing a
// package runs its preStart hook, and runs it once the install steps have
// been materialized.
//
// `cardano-up install` creates and starts a package's containers, so a
// package that has to seed state its service reads on first boot - Dolos
// bootstrapping storage from a Mithril snapshot, for example - needs a hook
// that runs during install but after the install steps that render its
// config. preInstall is too early: it runs before any install step, so a
// config rendered by a `file` step does not exist yet.
func TestInstall_RunsPreStartHookAfterInstallSteps(t *testing.T) {
	tmpDir := t.TempDir()
	hookLog := filepath.Join(tmpDir, "hooks.log")

	pkgName := "mypkg-1.0.0-testctx"
	renderedConfig := filepath.Join(tmpDir, pkgName, "daemon.toml")

	// The preStart hook fails unless the file install step has already
	// rendered its config, which is what makes this hook usable for seeding.
	preStart := writeHookScript(
		t, tmpDir, "prestart.sh", hookLog, "prestart",
		"test -f "+renderedConfig+" || exit 1\n",
	)
	preInstall := writeHookScript(t, tmpDir, "preinstall.sh", hookLog, "preinstall", "")
	postInstall := writeHookScript(t, tmpDir, "postinstall.sh", hookLog, "postinstall", "")

	cfg := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		CacheDir: tmpDir,
		DataDir:  tmpDir,
		BinDir:   tmpDir,
		Template: NewTemplate(nil),
	}
	pkg := Package{
		Name:              "mypkg",
		Version:           "1.0.0",
		PreInstallScript:  preInstall,
		PreStartScript:    preStart,
		PostInstallScript: postInstall,
		InstallSteps: []PackageInstallStep{
			{
				File: &PackageInstallStepFile{
					Filename: "daemon.toml",
					Content:  "seeded = true\n",
				},
			},
		},
	}

	if _, _, _, err := pkg.install(cfg, "testctx", nil, true, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	got := readHookLog(t, hookLog)
	want := []string{"preinstall", "prestart", "postinstall"}
	if len(got) != len(want) {
		t.Fatalf("unexpected hook order: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected hook order: got %v, want %v", got, want)
		}
	}
}

// TestInstall_SkipsPreStartHookWhenHooksDisabled verifies that the preStart
// hook honors the runHooks flag, which upgrade sets to false so that seeding
// work already done for the installed version is not repeated.
func TestInstall_SkipsPreStartHookWhenHooksDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	hookLog := filepath.Join(tmpDir, "hooks.log")
	preStart := writeHookScript(t, tmpDir, "prestart.sh", hookLog, "prestart", "")

	cfg := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		CacheDir: tmpDir,
		DataDir:  tmpDir,
		BinDir:   tmpDir,
		Template: NewTemplate(nil),
	}
	pkg := Package{
		Name:           "mypkg",
		Version:        "1.0.0",
		PreStartScript: preStart,
		InstallSteps:   []PackageInstallStep{},
	}

	if _, _, _, err := pkg.install(cfg, "testctx", nil, false, nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	if got := readHookLog(t, hookLog); len(got) != 0 {
		t.Fatalf("expected no hooks to run with runHooks=false, got %v", got)
	}
}

// TestStartService_PreStartHookRunsBeforeContainerStart verifies the ordering
// the seeding use case depends on: the preStart hook completes before any
// container is started. A hook that seeds storage is useless if the service
// that reads that storage has already booted.
func TestStartService_PreStartHookRunsBeforeContainerStart(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	tmpDir := t.TempDir()
	hookLog := filepath.Join(tmpDir, "hooks.log")
	preStart := writeHookScript(t, tmpDir, "prestart.sh", hookLog, "prestart", "")

	svc := &fakeServiceLifecycle{
		onStart: func() {
			f, err := os.OpenFile(
				hookLog,
				os.O_APPEND|os.O_CREATE|os.O_WRONLY,
				0o644,
			)
			if err != nil {
				t.Errorf("failed to record start: %v", err)
				return
			}
			defer f.Close()
			if _, err := f.WriteString("container-start\n"); err != nil {
				t.Errorf("failed to record start: %v", err)
			}
		},
	}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		return svc, nil
	}

	cfg := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: NewTemplate(nil),
	}
	pkg := Package{
		Name:           "mypkg",
		Version:        "1.0.0",
		PreStartScript: preStart,
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "svc",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	if err := pkg.startService(cfg, "testctx"); err != nil {
		t.Fatalf("startService failed: %v", err)
	}
	if !svc.started {
		t.Fatal("expected the container to be started")
	}

	got := readHookLog(t, hookLog)
	want := []string{"prestart", "container-start"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected order: got %v, want %v", got, want)
	}
}
