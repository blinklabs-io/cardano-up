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
	"strings"
	"testing"
)

const testArchiveFileContent = "#!/bin/sh\necho hello\n"

func buildTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := zw.SetComment("{{archive bytes must remain raw"); err != nil {
		t.Fatalf("unexpected error setting zip comment: %s", err)
	}
	for name, content := range entries {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatalf("unexpected error creating zip entry: %s", err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("unexpected error writing zip entry: %s", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("unexpected error closing zip writer: %s", err)
	}
	return buf.Bytes()
}

func buildTestTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatalf("unexpected error creating gzip writer: %s", err)
	}
	gzw.Comment = "{{archive bytes must remain raw"
	tw := tar.NewWriter(gzw)
	for name, content := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("unexpected error writing tar header: %s", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("unexpected error writing tar entry: %s", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("unexpected error closing tar writer: %s", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("unexpected error closing gzip writer: %s", err)
	}
	return buf.Bytes()
}

// TestExtractZipFile checks that the requested file is selected from a ZIP
// archive and returned with its original content.
func TestExtractZipFile(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"bin/mybinary": testArchiveFileContent,
		"README.md":    "docs",
	})

	content, err := extractZipFile("bin/mybinary", data)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if string(content) != testArchiveFileContent {
		t.Fatalf(
			"did not get expected content\n  got: %q\n  expected: %q",
			content,
			testArchiveFileContent,
		)
	}
}

// TestExtractZipFileNotFound checks that ZIP extraction returns an error when
// the requested file does not exist in the archive.
func TestExtractZipFileNotFound(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"bin/mybinary": testArchiveFileContent,
	})

	if _, err := extractZipFile("bin/missing", data); err == nil {
		t.Fatal("expected error for missing file in archive, got nil")
	}
}

// TestExtractZipFileSkipsDirs checks that a ZIP directory entry is ignored
// instead of being returned as an extracted file.
func TestExtractZipFileSkipsDirs(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("bin/"); err != nil {
		t.Fatalf("unexpected error creating zip dir entry: %s", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("unexpected error closing zip writer: %s", err)
	}

	if _, err := extractZipFile("bin", buf.Bytes()); err == nil {
		t.Fatal("expected error when requested path is a directory, got nil")
	}
}

// TestExtractTarGzFile checks that the requested file is selected from a
// tar.gz archive and returned with its original content.
func TestExtractTarGzFile(t *testing.T) {
	data := buildTestTarGz(t, map[string]string{
		"bin/mybinary": testArchiveFileContent,
		"README.md":    "docs",
	})

	content, err := extractTarGzFile("bin/mybinary", data)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if string(content) != testArchiveFileContent {
		t.Fatalf(
			"did not get expected content\n  got: %q\n  expected: %q",
			content,
			testArchiveFileContent,
		)
	}
}

// TestExtractTarGzFileNotFound checks that tar.gz extraction returns an error
// when the requested file does not exist in the archive.
func TestExtractTarGzFileNotFound(t *testing.T) {
	data := buildTestTarGz(t, map[string]string{
		"bin/mybinary": testArchiveFileContent,
	})

	if _, err := extractTarGzFile("bin/missing", data); err == nil {
		t.Fatal("expected error for missing file in archive, got nil")
	}
}

// TestExtractTarGzFileCorruptChecksum checks that extraction fails when the
// selected entry is readable but the gzip checksum is invalid.
func TestExtractTarGzFileCorruptChecksum(t *testing.T) {
	data := buildTestTarGz(t, map[string]string{
		"bin/mybinary": testArchiveFileContent,
	})
	data[len(data)-8] ^= 0xff

	if _, err := extractTarGzFile("bin/mybinary", data); err == nil {
		t.Fatal("expected error for invalid gzip checksum, got nil")
	}
}

// TestExtractArchiveFileDispatch checks that each supported archive name is
// routed to the correct extractor, including aliases and different casing.
func TestExtractArchiveFileDispatch(t *testing.T) {
	zipData := buildTestZip(t, map[string]string{"mybinary": testArchiveFileContent})
	tarGzData := buildTestTarGz(t, map[string]string{"mybinary": testArchiveFileContent})

	testDefs := []struct {
		archiveType string
		data        []byte
	}{
		{archiveType: "zip", data: zipData},
		{archiveType: "ZIP", data: zipData},
		{archiveType: "tar.gz", data: tarGzData},
		{archiveType: "tgz", data: tarGzData},
	}
	for _, testDef := range testDefs {
		content, err := extractArchiveFile(testDef.archiveType, "mybinary", testDef.data)
		if err != nil {
			t.Fatalf(
				"unexpected error for archive type %q: %s",
				testDef.archiveType,
				err,
			)
		}
		if string(content) != testArchiveFileContent {
			t.Fatalf(
				"did not get expected content for archive type %q\n  got: %q\n  expected: %q",
				testDef.archiveType,
				content,
				testArchiveFileContent,
			)
		}
	}
}

// TestExtractArchiveFileUnsupportedType checks that extraction fails clearly
// when an unsupported archive format is requested.
func TestExtractArchiveFileUnsupportedType(t *testing.T) {
	if _, err := extractArchiveFile("rar", "mybinary", nil); err == nil {
		t.Fatal("expected error for unsupported archive type, got nil")
	}
}

// TestValidArchiveType checks that supported archive names are accepted and
// unknown or empty archive names are rejected.
func TestValidArchiveType(t *testing.T) {
	validTypes := []string{"zip", "ZIP", "tar.gz", "TAR.GZ", "tgz"}
	for _, archiveType := range validTypes {
		if !validArchiveType(archiveType) {
			t.Errorf("expected %q to be a valid archive type", archiveType)
		}
	}
	invalidTypes := []string{"", "rar", "7z"}
	for _, archiveType := range invalidTypes {
		if validArchiveType(archiveType) {
			t.Errorf("expected %q to be an invalid archive type", archiveType)
		}
	}
}

// TestReadArchiveEntrySizeLimit checks that extraction stops with an error
// when an entry contains more data than the configured maximum size.
func TestReadArchiveEntrySizeLimit(t *testing.T) {
	reader := strings.NewReader(strings.Repeat("x", 11))

	if _, err := readArchiveEntry(reader, "mybinary", 10); err == nil {
		t.Fatal("expected error for oversized archive entry, got nil")
	}
}
