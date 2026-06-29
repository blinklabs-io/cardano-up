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
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"
)

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

	// Execute stopService and expect preStopScript to run
	if err := pkg.stopService(cfg, "testctx"); err != nil {
		t.Fatalf("stopService failed: %v", err)
	}

	// Validate all hook output
	hookOutput, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatalf("hook log file not found: %v", err)
	}
	if string(hookOutput) != "prestart executed\npoststart executed\nprestop executed\n" {
		t.Errorf(
			"unexpected hook output: got %q, want %q",
			string(hookOutput),
			"prestart executed\npoststart executed\nprestop executed\n",
		)
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
