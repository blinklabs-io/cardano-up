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
	"reflect"
	"strings"
	"testing"

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
