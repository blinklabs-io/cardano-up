// Copyright 2025 Blink Labs Software
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
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func registryPackages(cfg Config, validate bool) ([]Package, error) {
	if cfg.RegistryDir != "" {
		return registryPackagesDir(cfg, validate)
	} else if cfg.RegistryUrl != "" {
		return registryPackagesUrl(cfg, validate)
	} else {
		return nil, ErrNoRegistryConfigured
	}
}

func registryPackagesDir(cfg Config, validate bool) ([]Package, error) {
	tmpFs := os.DirFS(cfg.RegistryDir).(fs.ReadFileFS)
	return registryPackagesFs(cfg, tmpFs, validate)
}

func registryPackagesFs(
	cfg Config,
	filesystem fs.ReadFileFS,
	validate bool,
) ([]Package, error) {
	var ret []Package
	var retErr error
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
			fileReader, err := filesystem.Open(path)
			if err != nil {
				return err
			}
			tmpPkg, err := NewPackageFromReader(fileReader)
			if err != nil {
				if validate {
					// Record error for deferred failure
					retErr = ErrValidationFailed
				}
				cfg.Logger.Warn(
					fmt.Sprintf(
						"failed to load %q as package: %s",
						fullPath,
						err,
					),
				)
				return nil
			}
			// Skip "empty" packages
			if tmpPkg.Name == "" || tmpPkg.Version == "" {
				return nil
			}
			// Record on-disk path for package file
			// This is used for relative paths for external file references
			tmpPkg.filePath = fullPath
			// Add package to results
			ret = append(ret, tmpPkg)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return ret, retErr
}

func registryPackagesUrl(cfg Config, validate bool) ([]Package, error) {
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
		stat == nil ||
		stat.ModTime().Before(time.Now().Add(-24*time.Hour)) {
		// Fetch registry ZIP
		cfg.Logger.Info(
			"Fetching package registry " + cfg.RegistryUrl,
		)
		ctx := context.Background()
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			cfg.RegistryUrl,
			nil,
		)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("empty response from %s", cfg.RegistryUrl)
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
			// Ensure there are no parent dir references in path
			if strings.Contains(zipFile.Name, "..") {
				return nil, errors.New("parent path reference in zip name")
			}
			// #nosec G305
			outPath := filepath.Join(
				cachePath,
				zipFile.Name,
			)
			// Ensure our path is sane to prevent the gosec issue above
			if !strings.HasPrefix(outPath, filepath.Clean(cachePath)) {
				return nil, errors.New("zip extraction path mismatch")
			}
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
	return registryPackagesDir(cfg, validate)
}
