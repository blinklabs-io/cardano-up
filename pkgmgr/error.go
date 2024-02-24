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

import "errors"

// ErrOperationFailed is a placeholder error for operations that directly log errors.
// It's used to signify when an operation has failed when the actual error message is
// sent through the provided logger
var ErrOperationFailed = errors.New("the operation has failed")

// ErrMultipleInstallMethods is returned when a package's install steps specify more than one install method
// on a single install step
var ErrMultipleInstallMethods = errors.New("only one install method may be specified in an install step")

// ErrNoInstallMethods is returned when a package's install steps include an install step which has no
// recognized install method specified
var ErrNoInstallMethods = errors.New("no supported install method specified on install step")

// ErrContextNotExist is returned when trying to selecting/managing a context that does not exist
var ErrContextNotExist = errors.New("context does not exist")

// ErrContextAlreadyExists is returned when creating a context with a name that is already in use
var ErrContextAlreadyExists = errors.New("specified context already exists")

// ErrContainerAlreadyExists is returned when creating a new container with a name that is already in use
var ErrContainerAlreadyExists = errors.New("specified container already exists")
