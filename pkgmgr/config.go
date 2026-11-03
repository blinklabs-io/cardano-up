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
	"runtime"
)

var checkDockerConnectivity = CheckDockerConnectivity

type Config struct {
	BinDir              string
	CacheDir            string
	ConfigDir           string
	ContextDir          string
	DataDir             string
	Logger              *slog.Logger
	Template            *Template
	RequiredPackageTags []string
	RegistryUrl         string
	RegistryDir         string
	// Repositories are ordered from highest to lowest priority. A package
	// name found in an earlier repository hides packages with the same name
	// in later repositories, which allows a repository to overlay the
	// default package set.
	Repositories []Repository
}

// Repository identifies a package repository. Set exactly one of URL or Dir.
// Repositories are searched in declaration order.
type Repository struct {
	URL string
	Dir string
}

func NewDefaultConfig() (Config, error) {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf(
			"could not determine user home directory: %w",
			err,
		)
	}
	userBinDir := userHomeDir + "/.local/bin"
	userDataDir := userHomeDir + "/.local/share"
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return Config{}, fmt.Errorf(
			"could not determine user cache directory: %w",
			err,
		)
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf(
			"could not determine user config directory: %w",
			err,
		)
	}
	requiredPackageTags := []string{
		runtime.GOOS,
		runtime.GOARCH,
	}
	if checkDockerConnectivity() == nil {
		requiredPackageTags = append(requiredPackageTags, "docker")
	}
	ret := Config{
		BinDir: userBinDir,
		CacheDir: filepath.Join(
			userCacheDir,
			"cardano-up",
		),
		ConfigDir: filepath.Join(
			userConfigDir,
			"cardano-up",
		),
		DataDir: filepath.Join(
			userDataDir,
			"cardano-up",
		),
		Logger:              slog.Default(),
		RequiredPackageTags: requiredPackageTags,
		RegistryUrl:         "https://github.com/blinklabs-io/cardano-up-packages/archive/refs/heads/main.zip",
	}
	return ret, nil
}
