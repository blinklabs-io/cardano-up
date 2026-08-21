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

// TestStartServices_SkipsStepsWithFalseCondition verifies that starting a
// package's services honors the same install-step conditions the install loop
// applies. A step whose condition evaluates false is never materialized, so
// its container does not exist and inspecting it fails with
// ErrContainerNotExists - which would otherwise fail the whole operation.
func TestStartServices_SkipsStepsWithFalseCondition(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	created := &fakeServiceLifecycle{}
	var askedFor []string
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		askedFor = append(askedFor, containerName)
		if containerName == "mypkg-1.0.0-testctx-created" {
			return created, nil
		}
		// Matches production behavior for a container that was never created.
		return nil, ErrContainerNotExists
	}

	cfg := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: NewTemplate(nil),
	}
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "created",
					Image:         "alpine:3.20",
				},
			},
			{
				Condition: `eq "a" "b"`,
				Docker: &PackageInstallStepDocker{
					ContainerName: "skipped",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	if err := pkg.startService(cfg, "testctx"); err != nil {
		t.Fatalf("startService failed: %v", err)
	}
	if !created.started {
		t.Fatal("expected the materialized container to be started")
	}
	for _, name := range askedFor {
		if name == "mypkg-1.0.0-testctx-skipped" {
			t.Fatal("expected the condition-skipped step to not be looked up")
		}
	}
}

// TestStartServices_RollsBackWhenConditionErrors verifies that a condition
// that fails to evaluate rolls back services already started in the same
// call, rather than leaving a partial startup running.
func TestStartServices_RollsBackWhenConditionErrors(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	started := &fakeServiceLifecycle{}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		if containerName == "mypkg-1.0.0-testctx-started" {
			return started, nil
		}
		return nil, ErrContainerNotExists
	}

	cfg := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: NewTemplate(nil),
	}
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "started",
					Image:         "alpine:3.20",
				},
			},
			{
				// eq with a single argument fails at evaluation time.
				Condition: `eq "a"`,
				Docker: &PackageInstallStepDocker{
					ContainerName: "second",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	if err := pkg.startService(cfg, "testctx"); err == nil {
		t.Fatal("expected startService to fail when a condition cannot be evaluated")
	}
	if !started.started {
		t.Fatal("expected the first service to have been started")
	}
	if !started.stopped {
		t.Fatal("expected the already-started service to be rolled back")
	}
}

// TestConditionNeedsPackageTemplateVars pins the trap behind the condition
// filtering in services(): a template variable that is absent renders as
// false with no error, so a caller that passes a config without the
// package-specific variables does not fail loudly - it silently decides the
// condition is false and skips the step. Every caller of services() therefore
// has to build those variables first, or info and logs would quietly omit a
// service whose condition is actually true.
func TestConditionNeedsPackageTemplateVars(t *testing.T) {
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		Options: []PackageOption{
			{Name: "extra", Default: true},
		},
	}
	base := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		DataDir:  t.TempDir(),
		Template: NewTemplate(nil),
	}

	cfg := pkg.withPackageTemplateVars(base, "testctx", pkg.defaultOpts())
	ok, err := cfg.Template.EvaluateCondition(`.Package.Options.extra`, nil)
	if err != nil {
		t.Fatalf("unexpected error with package template vars: %v", err)
	}
	if !ok {
		t.Fatal("expected .Package.Options.extra to be true with package template vars")
	}

	ok, err = base.Template.EvaluateCondition(`.Package.Options.extra`, nil)
	if err != nil {
		t.Fatalf("unexpected error without package template vars: %v", err)
	}
	if ok {
		t.Fatal("expected the condition to evaluate false without package template vars")
	}
}

// TestServices_SkipsStepsWithFalseCondition verifies that services() honors
// install-step conditions, so a step that was never materialized is not
// looked up as a container.
func TestServices_SkipsStepsWithFalseCondition(t *testing.T) {
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		InstallSteps: []PackageInstallStep{
			{
				Condition: `eq "a" "b"`,
				Docker: &PackageInstallStepDocker{
					ContainerName: "skipped",
					Image:         "alpine:3.20",
				},
			},
		},
	}
	cfg := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: NewTemplate(nil),
	}

	services, err := pkg.services(cfg, "testctx")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected the condition-skipped step to yield no services, got %d", len(services))
	}
}

// TestStopService_SkipsStepsWithFalseCondition verifies that stopping a
// package honors install-step conditions, so the stop path does not look up a
// container that was never materialized. Without this, the first
// `cardano-up down` fails for a package that installs successfully.
func TestStopService_SkipsStepsWithFalseCondition(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	created := &fakeServiceLifecycle{running: true}
	var askedFor []string
	newServiceFromContainerName = func(
		containerName string,
		logger *slog.Logger,
	) (serviceLifecycle, error) {
		askedFor = append(askedFor, containerName)
		if containerName == "mypkg-1.0.0-testctx-created" {
			return created, nil
		}
		return nil, ErrContainerNotExists
	}

	cfg := Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: NewTemplate(nil),
	}
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "created",
					Image:         "alpine:3.20",
				},
			},
			{
				Condition: `eq "a" "b"`,
				Docker: &PackageInstallStepDocker{
					ContainerName: "skipped",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	if err := pkg.stopService(cfg, "testctx"); err != nil {
		t.Fatalf("stopService failed: %v", err)
	}
	if !created.stopped {
		t.Fatal("expected the materialized service to be stopped")
	}
	for _, name := range askedFor {
		if name == "mypkg-1.0.0-testctx-skipped" {
			t.Fatal("expected the condition-skipped step to not be looked up")
		}
	}
}

// TestInstalledPkgConfig_SuppliesConditionVars verifies that the config handed
// to condition-aware lifecycle calls carries the installed package's options.
// Uninstall, deactivate and stop all evaluate install-step conditions, and an
// absent variable reads as false without error - so passing the bare manager
// config makes uninstall skip a step whose container does exist, orphaning it
// while the installed-package record is removed.
func TestInstalledPkgConfig_SuppliesConditionVars(t *testing.T) {
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		Options: []PackageOption{
			{Name: "extra", Default: true},
		},
	}
	pm := &PackageManager{
		config: Config{
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			DataDir:  t.TempDir(),
			Template: NewTemplate(nil),
		},
	}
	installedPkg := InstalledPackage{
		Package: pkg,
		Context: "testctx",
		Options: map[string]bool{"extra": true},
	}

	cfg := pm.installedPkgConfig(installedPkg, "testctx")
	ok, err := cfg.Template.EvaluateCondition(`.Package.Options.extra`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected the installed package's options to be visible to the condition")
	}

	// The bare manager config is what caused the orphaning: silently false.
	ok, err = pm.config.Template.EvaluateCondition(`.Package.Options.extra`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected the bare manager config to evaluate the condition false")
	}
}
