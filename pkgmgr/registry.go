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
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func registryPackages(cfg Config) ([]Package, error) {
	if cfg.RegistryDir != "" {
		return registryPackagesDir(cfg)
	} else if cfg.RegistryUrl != "" {
		return registryPackagesUrl(cfg)
	} else {
		return nil, ErrNoRegistryConfigured
	}
}

func registryPackagesDir(cfg Config) ([]Package, error) {
	tmpFs := os.DirFS(cfg.RegistryDir).(fs.ReadFileFS)
	return registryPackagesFs(cfg, tmpFs)
}

func registryPackagesFs(cfg Config, filesystem fs.ReadFileFS) ([]Package, error) {
	var ret []Package
	absRegistryDir, err := filepath.Abs(cfg.RegistryDir)
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(
		filesystem,
		`.`,
		func(path string, d fs.DirEntry, err error) error {
			// Replacing leading dot with registry dir
			fullPath := filepath.Join(
				absRegistryDir,
				path,
			)
			if err != nil {
				return err
			}
			// Skip dirs
			if d.IsDir() {
				// Skip all files inside dot-dirs
				if strings.HasPrefix(d.Name(), `.`) && d.Name() != `.` {
					return fs.SkipDir
				}
				return nil
			}
			// Skip non-YAML files based on file extension
			if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
				return nil
			}
			// Try to parse YAML file as package
			fileData, err := filesystem.ReadFile(path)
			if err != nil {
				return err
			}
			var tmpPkg Package
			if err := yaml.Unmarshal(fileData, &tmpPkg); err != nil {
				cfg.Logger.Warn(
					fmt.Sprintf(
						"failed to load %q as package: %s",
						fullPath,
						err,
					),
				)
				return nil
			}
			if tmpPkg.Name == "" || tmpPkg.Version == "" {
				return nil
			}
			ret = append(ret, tmpPkg)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func registryPackagesUrl(cfg Config) ([]Package, error) {
	cachePath := filepath.Join(
		cfg.CacheDir,
		"registry",
	)
	// Check age of existing cache
	stat, err := os.Stat(cachePath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	// Fetch and extract registry ZIP into cache if it doesn't exist or is too old
	if errors.Is(err, fs.ErrNotExist) ||
		stat.ModTime().Before(time.Now().Add(-24*time.Hour)) {
		// Fetch registry ZIP
		cfg.Logger.Info(
			fmt.Sprintf("Fetching package registry %s", cfg.RegistryUrl),
		)
		resp, err := http.Get(cfg.RegistryUrl)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		zipData := bytes.NewReader(respBody)
		zipReader, err := zip.NewReader(
			zipData,
			int64(zipData.Len()),
		)
		if err != nil {
			return nil, err
		}
		// Clear out existing cache files
		if err := os.RemoveAll(cachePath); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(cachePath, fs.ModePerm); err != nil {
			return nil, err
		}
		// Extract files from ZIP into cache path
		for _, zipFile := range zipReader.File {
			// Skip directory entries
			if (zipFile.Mode() & fs.ModeDir) > 0 {
				continue
			}
			outPath := filepath.Join(
				cachePath,
				zipFile.Name,
			)
			// Create parent dir(s)
			if err := os.MkdirAll(filepath.Dir(outPath), fs.ModePerm); err != nil {
				return nil, err
			}
			// Read file bytes
			zf, err := zipFile.Open()
			if err != nil {
				return nil, err
			}
			zfData, err := io.ReadAll(zf)
			if err != nil {
				return nil, err
			}
			zf.Close()
			// Write file
			if err := os.WriteFile(outPath, zfData, fs.ModePerm); err != nil {
				return nil, err
			}
		}
	}
	// Process cache dir
	cfg.RegistryDir = cachePath
	return registryPackagesDir(cfg)
}
