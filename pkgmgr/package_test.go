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
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"
)

type fakeServiceLifecycle struct {
	running    bool
	started    bool
	stopped    bool
	stopCalled bool

	// stopErr, if set, is returned by Stop(). By default this simulates a
	// total stop failure (the container stays running). Set
	// stopErrLeavesStopped to simulate a partial failure where the
	// container actually stopped despite Stop() reporting an error.
	stopErr              error
	stopErrLeavesStopped bool

	// stopNoop, if true, makes Stop() report success without actually
	// changing the container's running state - simulates a stop call that
	// silently fails to take effect.
	stopNoop bool

	// runningErrAfterStop, if set, is returned by Running() only once
	// Stop() has been called - simulates a status check that fails right
	// after an attempt to stop the container.
	runningErrAfterStop error
}

func (f *fakeServiceLifecycle) Running() (bool, error) {
	if f.stopCalled && f.runningErrAfterStop != nil {
		return false, f.runningErrAfterStop
	}
	return f.running, nil
}

func (f *fakeServiceLifecycle) Start() error {
	f.started = true
	f.running = true
	return nil
}

func (f *fakeServiceLifecycle) Stop() error {
	f.stopCalled = true
	if f.stopNoop {
		return nil
	}
	if f.stopErr != nil {
		if f.stopErrLeavesStopped {
			f.stopped = true
			f.running = false
		}
		return f.stopErr
	}
	f.stopped = true
	f.running = false
	return nil
}

var packageTestDefs = []struct {
	yaml       string
	packageObj Package
}{
	{
		yaml: "name: foo\nversion: 1.2.3",
		packageObj: Package{
			Name:    "foo",
			Version: "1.2.3",
		},
	},
}

func TestNewPackageFromReader(t *testing.T) {
	for _, testDef := range packageTestDefs {
		r := strings.NewReader(testDef.yaml)
		p, err := NewPackageFromReader(r)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !reflect.DeepEqual(p, testDef.packageObj) {
			t.Fatalf(
				"did not get expected package object\n  got: %#v\n  expected: %#v",
				p,
				testDef.packageObj,
			)
		}
	}
}

func TestPackageToYaml(t *testing.T) {
	for _, testDef := range packageTestDefs {
		data, err := yaml.Marshal(&testDef.packageObj)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		trimmedData := strings.TrimRight(string(data), "\r\n")
		if trimmedData != testDef.yaml {
			t.Fatalf(
				"did not get expected package YAML\n  got: %s\n  expected: %s",
				trimmedData,
				testDef.yaml,
			)
		}
	}
}

func TestOSAndARCH(t *testing.T) {
	expectOS := runtime.GOOS
	expectARCH := runtime.GOARCH

	// Initialized a config object
	tempCacheDir := t.TempDir()
	tempDataDir := t.TempDir()
	cfg := Config{
		CacheDir: tempCacheDir,
		DataDir:  tempDataDir,
		BinDir:   "/usr/local/bin",
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}

	cfg.Template = cfg.Template.WithVars(
		map[string]any{
			"System": map[string]string{
				"OS":   runtime.GOOS,
				"ARCH": runtime.GOARCH,
			},
		},
	)

	// Defined a test package
	pkg := Package{}
	pkg.Name = "test-package"
	pkg.Version = "1.0.0"
	opts := make(map[string]bool)

	_, _, _, err := pkg.install(cfg, "test", opts, false, nil)
	if err != nil {
		t.Errorf("Installation failed: %v", err)
	}

	// Verify if OS and ARCH are injected into the config template
	actualOS, err := cfg.Template.Render("{{ .System.OS }}", nil)
	if err != nil {
		t.Errorf("Template rendering for OS failed: %v", err)
	} else if actualOS != expectOS {
		t.Errorf("Expected OS: %s and rendered OS: %s are not same", expectOS, actualOS)
	} else {
		t.Logf("Expected OS matched with rendered OS")
	}

	actualARCH, err := cfg.Template.Render("{{ .System.ARCH }}", nil)
	if err != nil {
		t.Errorf("Template rendering for ARCH failed: %v", err)
	} else if actualARCH != expectARCH {
		t.Errorf("Expected ARCH: %s and rendered ARCH: %s are not same", expectARCH, actualARCH)
	} else {
		t.Logf("Expected ARCH matched with rendered ARCH")
	}

	if actualOS == expectOS && actualARCH == expectARCH {
		t.Logf(
			"Test is successful and OS, ARCH values are correctly injected to config template",
		)
	}
}

// TestServiceHooks_PreStartPostStartAndPreStop verifies that startService
// and stopService run their hook scripts in the correct order: preStart and
// postStart around a start, then preStop and postStop around a stop.
func TestServiceHooks_PreStartPostStartAndPreStop(t *testing.T) {
	tmpDir := t.TempDir()
	hookLog := filepath.Join(tmpDir, "hooks.log")

	// Create preStart script
	preStartScript := filepath.Join(tmpDir, "prestart.sh")
	preStartContent := "#!/bin/sh\necho 'prestart executed' >> " + hookLog
	if err := os.WriteFile(preStartScript, []byte(preStartContent), 0755); err != nil {
		t.Fatalf("failed to write preStart script: %v", err)
	}

	// Create postStart script
	postStartScript := filepath.Join(tmpDir, "poststart.sh")
	postStartContent := "#!/bin/sh\necho 'poststart executed' >> " + hookLog
	if err := os.WriteFile(postStartScript, []byte(postStartContent), 0755); err != nil {
		t.Fatalf("failed to write postStart script: %v", err)
	}

	// Create preStop script
	preStopScript := filepath.Join(tmpDir, "prestop.sh")
	preStopContent := "#!/bin/sh\necho 'prestop executed' >> " + hookLog
	if err := os.WriteFile(preStopScript, []byte(preStopContent), 0755); err != nil {
		t.Fatalf("failed to write preStop script: %v", err)
	}

	// Create postStop script
	postStopScript := filepath.Join(tmpDir, "poststop.sh")
	postStopContent := "#!/bin/sh\necho 'poststop executed' >> " + hookLog
	if err := os.WriteFile(postStopScript, []byte(postStopContent), 0755); err != nil {
		t.Fatalf("failed to write postStop script: %v", err)
	}

	// Initialize a config object
	cfg := Config{
		CacheDir: tmpDir,
		DataDir:  tmpDir,
		BinDir:   tmpDir,
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	// Define a test package
	pkg := Package{
		Name:            "mypkg",
		Version:         "1.0.0",
		PreStartScript:  preStartScript,
		PostStartScript: postStartScript,
		PreStopScript:   preStopScript,
		PostStopScript:  postStopScript,
		InstallSteps:    []PackageInstallStep{},
	}

	// Execute startService and expect preStartScript to run
	if err := pkg.startService(cfg, "testctx"); err != nil {
		t.Fatalf("startService failed: %v", err)
	}

	// Validate start hook output
	startOutput, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatalf("hook log file not found: %v", err)
	}
	if string(startOutput) != "prestart executed\npoststart executed\n" {
		t.Errorf(
			"unexpected start hook output: got %q, want %q",
			string(startOutput),
			"prestart executed\npoststart executed\n",
		)
	}

	// Execute stopService and expect preStopScript and postStopScript to run
	if err := pkg.stopService(cfg, "testctx"); err != nil {
		t.Fatalf("stopService failed: %v", err)
	}

	// Validate all hook output
	hookOutput, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatalf("hook log file not found: %v", err)
	}
	if string(hookOutput) != "prestart executed\npoststart executed\nprestop executed\npoststop executed\n" {
		t.Errorf(
			"unexpected hook output: got %q, want %q",
			string(hookOutput),
			"prestart executed\npoststart executed\nprestop executed\npoststop executed\n",
		)
	}
}

// TestStopService_PostStopHookSkippedOnStopFailure verifies that
// postStopScript does not run when a Docker container fails to stop,
// matching the conservative skip-on-failure behavior of postStartScript. It
// also verifies that a container already stopped earlier in the same call
// is rolled back (restarted) when a later container fails to stop.
func TestStopService_PostStopHookSkippedOnStopFailure(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	stoppedSvc := &fakeServiceLifecycle{running: true}
	brokenSvc := &fakeServiceLifecycle{
		running: true,
		stopErr: errors.New("simulated stop failure"),
	}
	services := map[string]*fakeServiceLifecycle{
		"mypkg-1.0.0-testctx-stopped": stoppedSvc,
		"mypkg-1.0.0-testctx-broken":  brokenSvc,
	}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		return services[containerName], nil
	}

	tmpDir := t.TempDir()
	hookLog := filepath.Join(tmpDir, "hooks.log")
	postStopScript := filepath.Join(tmpDir, "poststop.sh")
	postStopContent := "#!/bin/sh\necho 'poststop executed' >> " + hookLog
	if err := os.WriteFile(postStopScript, []byte(postStopContent), 0755); err != nil {
		t.Fatalf("failed to write postStop script: %v", err)
	}

	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	pkg := Package{
		Name:           "mypkg",
		Version:        "1.0.0",
		PostStopScript: postStopScript,
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "stopped",
					Image:         "alpine:3.20",
				},
			},
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "broken",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	err := pkg.stopService(cfg, "testctx")
	if err == nil {
		t.Fatal("expected stopService to fail when a container fails to stop")
	}
	if _, statErr := os.Stat(hookLog); !os.IsNotExist(statErr) {
		t.Fatal("expected postStopScript to be skipped when stopping a service fails")
	}
	if !stoppedSvc.started {
		t.Fatal("expected the earlier successfully-stopped service to be rolled back (restarted)")
	}
	if brokenSvc.started {
		t.Fatal("expected the service that failed to stop to not be started during rollback")
	}
}

// TestStopService_DoesNotRollBackOnAmbiguousStopError verifies that when
// Stop() itself returns an error, stopService does not attempt to roll back
// (restart) that container, even if it later observes the container as
// stopped. A container found stopped after a failed Stop() call could have
// been stopped by a concurrent, unrelated operation, and restarting it would
// risk undoing that other operation's intended effect. Only a Stop() call
// this code issued and confirmed successful is eligible for rollback.
func TestStopService_DoesNotRollBackOnAmbiguousStopError(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	ambiguousSvc := &fakeServiceLifecycle{
		running:              true,
		stopErr:              errors.New("stop reported an error but the container ended up stopped"),
		stopErrLeavesStopped: true,
	}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		return ambiguousSvc, nil
	}

	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "flaky",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	err := pkg.stopService(cfg, "testctx")
	if err == nil {
		t.Fatal("expected stopService to fail")
	}
	if ambiguousSvc.started {
		t.Fatal("expected no rollback for a Stop() call that reported an error, to avoid undoing a possibly-concurrent stop")
	}
}

// TestStopService_TreatsStillRunningAfterStopAsFailure verifies that a
// container still observed as running after Stop() reports success is
// treated as a stop failure: postStopScript is skipped, an error is
// returned, and the container is not "rolled back" since it never actually
// stopped in the first place.
func TestStopService_TreatsStillRunningAfterStopAsFailure(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	stuckSvc := &fakeServiceLifecycle{running: true, stopNoop: true}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		return stuckSvc, nil
	}

	tmpDir := t.TempDir()
	hookLog := filepath.Join(tmpDir, "hooks.log")
	postStopScript := filepath.Join(tmpDir, "poststop.sh")
	postStopContent := "#!/bin/sh\necho 'poststop executed' >> " + hookLog
	if err := os.WriteFile(postStopScript, []byte(postStopContent), 0755); err != nil {
		t.Fatalf("failed to write postStop script: %v", err)
	}

	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	pkg := Package{
		Name:           "mypkg",
		Version:        "1.0.0",
		PostStopScript: postStopScript,
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "stuck",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	err := pkg.stopService(cfg, "testctx")
	if err == nil {
		t.Fatal("expected stopService to fail when the container is still running after Stop()")
	}
	if _, statErr := os.Stat(hookLog); !os.IsNotExist(statErr) {
		t.Fatal("expected postStopScript to be skipped when a container is still running")
	}
	if stuckSvc.started {
		t.Fatal("expected the still-running container not to be rolled back, since it was never actually stopped")
	}
}

// TestStopService_RollsBackOnUnknownStateAfterStopFailure verifies that when
// the Running() status check fails right after a Stop() call, stopService
// still attempts a best-effort rollback of that container rather than
// leaving it stopped with an unknown final state.
func TestStopService_RollsBackOnUnknownStateAfterStopFailure(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	flakySvc := &fakeServiceLifecycle{
		running:             true,
		runningErrAfterStop: errors.New("status check failed after stop"),
	}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		return flakySvc, nil
	}

	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	pkg := Package{
		Name:    "mypkg",
		Version: "1.0.0",
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "flaky",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	err := pkg.stopService(cfg, "testctx")
	if err == nil {
		t.Fatal("expected stopService to fail")
	}
	if !flakySvc.started {
		t.Fatal("expected best-effort rollback to restart a container whose final state is unknown")
	}
}

// TestStopService_PostStopHookFailure verifies that stopService surfaces a
// wrapped "post-stop hook failed" error when postStopScript exits non-zero.
func TestStopService_PostStopHookFailure(t *testing.T) {
	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	pkg := Package{
		Name:           "mypkg",
		Version:        "1.0.0",
		PostStopScript: "exit 1",
		InstallSteps:   []PackageInstallStep{},
	}

	err := pkg.stopService(cfg, "testctx")
	if err == nil {
		t.Fatal("expected post-stop hook failure")
	}
	if !strings.Contains(err.Error(), "post-stop hook failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestStopService_RollsBackStoppedServicesOnPostStopFailure verifies that
// when postStopScript fails, stopService restarts only the containers it
// stopped during this call, leaving already-stopped containers untouched.
func TestStopService_RollsBackStoppedServicesOnPostStopFailure(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	stoppedSvc := &fakeServiceLifecycle{running: true}
	alreadyStoppedSvc := &fakeServiceLifecycle{running: false}
	services := map[string]*fakeServiceLifecycle{
		"mypkg-1.0.0-testctx-running": stoppedSvc,
		"mypkg-1.0.0-testctx-stopped": alreadyStoppedSvc,
	}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		return services[containerName], nil
	}

	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	pkg := Package{
		Name:           "mypkg",
		Version:        "1.0.0",
		PostStopScript: "exit 1",
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "running",
					Image:         "alpine:3.20",
				},
			},
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "stopped",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	err := pkg.stopService(cfg, "testctx")
	if err == nil {
		t.Fatal("expected post-stop hook failure")
	}
	if !strings.Contains(err.Error(), "post-stop hook failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stoppedSvc.stopped {
		t.Fatal("expected running service to be stopped")
	}
	if !stoppedSvc.started {
		t.Fatal("expected newly-stopped service to be restarted during rollback")
	}
	if alreadyStoppedSvc.started {
		t.Fatal("expected already-stopped service to be left stopped")
	}
}

func TestStartService_RollsBackStartedServicesOnPostStartFailure(t *testing.T) {
	origNewServiceFromContainerName := newServiceFromContainerName
	t.Cleanup(func() {
		newServiceFromContainerName = origNewServiceFromContainerName
	})

	startedSvc := &fakeServiceLifecycle{}
	alreadyRunningSvc := &fakeServiceLifecycle{running: true}
	services := map[string]*fakeServiceLifecycle{
		"mypkg-1.0.0-testctx-started": startedSvc,
		"mypkg-1.0.0-testctx-running": alreadyRunningSvc,
	}
	newServiceFromContainerName = func(containerName string, logger *slog.Logger) (serviceLifecycle, error) {
		return services[containerName], nil
	}

	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Template: &Template{
			tmpl:     template.New("test"),
			baseVars: make(map[string]any),
		},
	}
	pkg := Package{
		Name:            "mypkg",
		Version:         "1.0.0",
		PostStartScript: "exit 1",
		InstallSteps: []PackageInstallStep{
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "started",
					Image:         "alpine:3.20",
				},
			},
			{
				Docker: &PackageInstallStepDocker{
					ContainerName: "running",
					Image:         "alpine:3.20",
				},
			},
		},
	}

	err := pkg.startService(cfg, "testctx")
	if err == nil {
		t.Fatal("expected post-start hook failure")
	}
	if !strings.Contains(err.Error(), "post-start hook failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !startedSvc.started {
		t.Fatal("expected stopped service to be started")
	}
	if !startedSvc.stopped {
		t.Fatal("expected newly-started service to be stopped during rollback")
	}
	if alreadyRunningSvc.stopped {
		t.Fatal("expected already-running service to be left running")
	}
}

// runHookWithStdin runs the given hook script with the provided file as the
// process stdin, capturing whatever the hook writes to stdout/stderr.
//
// runHookScript wires the child's stdio to the os.Std* package vars, so the
// only way to exercise that behavior is to swap those globals for the duration
// of the call. That makes these tests inherently non-parallel.
func runHookWithStdin(
	t *testing.T,
	stdin *os.File,
	script string,
) (string, error) {
	t.Helper()

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	// Drain the read end concurrently so a hook that produces more output than
	// the pipe buffer can hold does not deadlock against cmd.Wait().
	outCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outCh <- buf.String()
	}()

	origIn, origOut, origErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = stdin, wOut, wOut
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = origIn, origOut, origErr
	}()

	cfg := Config{Template: NewTemplate(nil)}
	runErr := Package{}.runHookScript(cfg, script)

	// Close our copy of the write end so the drain goroutine sees EOF.
	_ = wOut.Close()
	out := <-outCh
	_ = rOut.Close()

	return out, runErr
}

func TestRunHookScriptForwardsStdin(t *testing.T) {
	// A regular file gives the hook a natural EOF, so `cat` reads it and exits.
	inPath := filepath.Join(t.TempDir(), "stdin")
	want := "hello from stdin\n"
	if err := os.WriteFile(inPath, []byte(want), 0o600); err != nil {
		t.Fatalf("failed to write stdin file: %v", err)
	}
	f, err := os.Open(inPath)
	if err != nil {
		t.Fatalf("failed to open stdin file: %v", err)
	}
	defer f.Close()

	got, err := runHookWithStdin(t, f, "cat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("stdin was not forwarded to the hook: got %q, want %q", got, want)
	}
}

func TestRunHookScriptSuccess(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}
	defer f.Close()

	if _, err := runHookWithStdin(t, f, "exit 0"); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestRunHookScriptError(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}
	defer f.Close()

	_, err = runHookWithStdin(t, f, "exit 7")
	if err == nil {
		t.Fatal("expected a non-zero exit to return an error, got nil")
	}
	if !strings.Contains(err.Error(), "exited with error") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
