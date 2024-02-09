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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

type Config struct {
	ConfigDir string
	CacheDir  string
	Logger    *slog.Logger
}

func NewDefaultConfig() (Config, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf(
			"could not determine user config directory: %s",
			err,
		)
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return Config{}, fmt.Errorf(
			"could not determine user cache directory: %s",
			err,
		)
	}
	ret := Config{
		ConfigDir: filepath.Join(
			userConfigDir,
			"cardano-up",
		),
		CacheDir: filepath.Join(
			userCacheDir,
			"cardano-up",
		),
		Logger: slog.Default(),
	}
	return ret, nil
}
