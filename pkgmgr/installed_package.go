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
	"time"
)

type InstalledPackage struct {
	InstalledTime    time.Time
	Options          map[string]bool
	Outputs          map[string]string
	Package          Package
	Context          string
	PostInstallNotes string
}

func NewInstalledPackage(
	pkg Package,
	context string,
	postInstallNotes string,
	outputs map[string]string,
	options map[string]bool,
) InstalledPackage {
	return InstalledPackage{
		Package:          pkg,
		InstalledTime:    time.Now(),
		Context:          context,
		PostInstallNotes: postInstallNotes,
		Options:          options,
		Outputs:          outputs,
	}
}

func (i InstalledPackage) IsEmpty() bool {
	return i.InstalledTime.IsZero()
}
