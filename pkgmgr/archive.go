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
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Supported values for the file install step's archive field
const (
	archiveTypeZip   = "zip"
	archiveTypeTarGz = "tar.gz"
	archiveTypeTgz   = "tgz"
)

// validArchiveType returns whether archiveType is a supported archive type
// for the file install step
func validArchiveType(archiveType string) bool {
	switch strings.ToLower(archiveType) {
	case archiveTypeZip, archiveTypeTarGz, archiveTypeTgz:
		return true
	default:
		return false
	}
}

// extractArchiveFile returns the content of the file at archivePath within
// the archive represented by data. The archive is expected to be in the
// format specified by archiveType (zip, tar.gz, or tgz)
func extractArchiveFile(
	archiveType string,
	archivePath string,
	data []byte,
) ([]byte, error) {
	switch strings.ToLower(archiveType) {
	case archiveTypeZip:
		return extractZipFile(archivePath, data)
	case archiveTypeTarGz, archiveTypeTgz:
		return extractTarGzFile(archivePath, data)
	default:
		return nil, fmt.Errorf("unsupported archive type %q", archiveType)
	}
}

func extractZipFile(archivePath string, data []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to read zip archive: %w", err)
	}
	cleanPath := filepath.Clean(archivePath)
	for _, zipFile := range zipReader.File {
		if zipFile.FileInfo().IsDir() {
			continue
		}
		if filepath.Clean(zipFile.Name) != cleanPath {
			continue
		}
		zf, err := zipFile.Open()
		if err != nil {
			return nil, err
		}
		defer zf.Close()
		return io.ReadAll(zf)
	}
	return nil, fmt.Errorf("file %q not found in zip archive", archivePath)
}

func extractTarGzFile(archivePath string, data []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to read gzip data: %w", err)
	}
	defer gzipReader.Close()
	cleanPath := filepath.Clean(archivePath)
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Clean(header.Name) != cleanPath {
			continue
		}
		return io.ReadAll(tarReader)
	}
	return nil, fmt.Errorf("file %q not found in tar.gz archive", archivePath)
}
