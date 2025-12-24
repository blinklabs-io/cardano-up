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

func TestServiceHooks_PreStartAndPreStop(t *testing.T) {
	tmpDir := t.TempDir()

	// Create preStart script
	preStartLog := filepath.Join(tmpDir, "prestart.log")
	preStartScript := filepath.Join(tmpDir, "prestart.sh")
	preStartContent := "#!/bin/sh\necho 'prestart executed' > " + preStartLog
	if err := os.WriteFile(preStartScript, []byte(preStartContent), 0755); err != nil {
		t.Fatalf("failed to write preStart script: %v", err)
	}

	// Create preStop script
	preStopLog := filepath.Join(tmpDir, "prestop.log")
	preStopScript := filepath.Join(tmpDir, "prestop.sh")
	preStopContent := "#!/bin/sh\necho 'prestop executed' > " + preStopLog
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
		Name:           "mypkg",
		Version:        "1.0.0",
		PreStartScript: preStartScript,
		PreStopScript:  preStopScript,
		InstallSteps:   []PackageInstallStep{},
	}

	// Execute startService and expect preStartScript to run
	if err := pkg.startService(cfg, "testctx"); err != nil {
		t.Fatalf("startService failed: %v", err)
	}

	// Validate preStart script output
	preStartOutput, err := os.ReadFile(preStartLog)
	if err != nil {
		t.Fatalf("preStart log file not found: %v", err)
	}
	if string(preStartOutput) != "prestart executed\n" {
		t.Errorf(
			"unexpected preStart output: got %q, want %q",
			string(preStartOutput),
			"prestart executed\n",
		)
	}

	// Execute stopService and expect preStopScript to run
	if err := pkg.stopService(cfg, "testctx"); err != nil {
		t.Fatalf("stopService failed: %v", err)
	}

	// Validate preStop script output
	preStopOutput, err := os.ReadFile(preStopLog)
	if err != nil {
		t.Fatalf("preStop log file not found: %v", err)
	}
	if string(preStopOutput) != "prestop executed\n" {
		t.Errorf(
			"unexpected preStop output: got %q, want %q",
			string(preStopOutput),
			"prestop executed\n",
		)
	}
}
