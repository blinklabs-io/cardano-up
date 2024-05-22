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
	"testing"
)

func TestSplitPackage(t *testing.T) {
	testDefs := []struct {
		Package     string
		Name        string
		VersionSpec string
		Options     map[string]bool
	}{
		{
			Package:     "test-packageB[foo,-bar] >= 1.2.3",
			Name:        "test-packageB",
			VersionSpec: ">= 1.2.3",
			Options: map[string]bool{
				"foo": true,
				"bar": false,
			},
		},
		{
			Package:     "test-package<1.2.4",
			Name:        "test-package",
			VersionSpec: "<1.2.4",
		},
		{
			Package: "test-package",
			Name:    "test-package",
		},
		{
			Package: "test-package[foo",
			Name:    "test-package[foo",
		},
	}
	for _, testDef := range testDefs {
		tmpResolver := &Resolver{}
		pkgName, pkgVersionSpec, pkgOpts := tmpResolver.splitPackage(
			testDef.Package,
		)
		if pkgName != testDef.Name {
			t.Fatalf(
				"did not get expected package name: got %q, expected %q",
				pkgName,
				testDef.Name,
			)
		}
		if pkgVersionSpec != testDef.VersionSpec {
			t.Fatalf(
				"did not get expected package version spec: got %q, expected %q",
				pkgVersionSpec,
				testDef.VersionSpec,
			)
		}
		if len(pkgOpts) > 0 && len(testDef.Options) > 0 {
			if !reflect.DeepEqual(pkgOpts, testDef.Options) {
				t.Fatalf(
					"did not get expected package options\n  got: %#v\n  expected: %#v",
					pkgOpts,
					testDef.Options,
				)
			}
		}
	}
}
