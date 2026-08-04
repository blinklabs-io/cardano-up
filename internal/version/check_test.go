// Copyright 2026 Blink Labs Software
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

package version

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCheckForUpdateFetchesAndCachesLatestRelease checks that a first call
// hits the GitHub API and writes a cache file, and that a second call within
// the TTL reuses the cache instead of making another request.
func TestCheckForUpdateFetchesAndCachesLatestRelease(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			if got, want := r.Header.Get("Accept"), "application/vnd.github+json"; got != want {
				t.Errorf("unexpected Accept header: got %q, want %q", got, want)
			}
			if got, want := r.Header.Get("X-GitHub-Api-Version"), "2026-03-10"; got != want {
				t.Errorf("unexpected API version header: got %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(
				`{"tag_name":"v1.2.0","html_url":"https://example.test/v1.2.0"}`,
			))
		},
	))
	defer server.Close()

	cacheDir := t.TempDir()
	now := time.Now()
	update, err := checkForUpdate(
		context.Background(),
		"v1.1.0",
		cacheDir,
		server.URL,
		server.Client(),
		now,
	)
	if err != nil {
		t.Fatalf("unexpected version check error: %s", err)
	}
	if update == nil {
		t.Fatal("expected an available update")
	}
	if got, want := update.LatestVersion, "v1.2.0"; got != want {
		t.Fatalf("unexpected latest version: got %q, want %q", got, want)
	}
	if requestCount != 1 {
		t.Fatalf("unexpected request count: got %d, want 1", requestCount)
	}

	update, err = checkForUpdate(
		context.Background(),
		"v1.1.0",
		cacheDir,
		server.URL,
		server.Client(),
		now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("unexpected cached version check error: %s", err)
	}
	if update == nil || update.LatestVersion != "v1.2.0" {
		t.Fatalf("unexpected cached update: %#v", update)
	}
	if requestCount != 1 {
		t.Fatalf("fresh cache should avoid another request, got %d requests", requestCount)
	}
}

// TestCheckForUpdateRefreshesStaleCache checks that a cache file older than
// the TTL is treated as expired and triggers a fresh request instead of
// being reused.
func TestCheckForUpdateRefreshesStaleCache(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, versionCacheFile)
	if err := os.WriteFile(
		cachePath,
		[]byte(`{"tag_name":"v1.1.0","html_url":"https://example.test/v1.1.0"}`),
		0o600,
	); err != nil {
		t.Fatalf("failed to write test cache: %s", err)
	}
	oldTime := time.Now().Add(-versionCacheTTL - time.Hour)
	if err := os.Chtimes(cachePath, oldTime, oldTime); err != nil {
		t.Fatalf("failed to age test cache: %s", err)
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			_, _ = w.Write([]byte(
				`{"tag_name":"v1.3.0","html_url":"https://example.test/v1.3.0"}`,
			))
		},
	))
	defer server.Close()

	update, err := checkForUpdate(
		context.Background(),
		"v1.2.0",
		cacheDir,
		server.URL,
		server.Client(),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("unexpected version check error: %s", err)
	}
	if update == nil || update.LatestVersion != "v1.3.0" {
		t.Fatalf("unexpected refreshed update: %#v", update)
	}
	if requestCount != 1 {
		t.Fatalf("stale cache should be refreshed, got %d requests", requestCount)
	}
}

// TestCheckForUpdateReplacesInvalidCache checks that a cache file containing
// unparseable content is ignored and replaced with a fresh request, rather
// than surfacing an error.
func TestCheckForUpdateReplacesInvalidCache(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, versionCacheFile)
	if err := os.WriteFile(cachePath, []byte(`not-json`), 0o600); err != nil {
		t.Fatalf("failed to write invalid test cache: %s", err)
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			_, _ = w.Write([]byte(
				`{"tag_name":"v1.3.0","html_url":"https://example.test/v1.3.0"}`,
			))
		},
	))
	defer server.Close()

	update, err := checkForUpdate(
		context.Background(),
		"v1.2.0",
		cacheDir,
		server.URL,
		server.Client(),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("unexpected version check error: %s", err)
	}
	if update == nil || update.LatestVersion != "v1.3.0" {
		t.Fatalf("unexpected update after invalid cache: %#v", update)
	}
	if requestCount != 1 {
		t.Fatalf("invalid cache should be replaced, got %d requests", requestCount)
	}
}

// TestCheckForUpdateReturnsNilForCurrentVersion checks that no update is
// reported when the running version matches the latest published release.
func TestCheckForUpdateReturnsNilForCurrentVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(
				`{"tag_name":"v1.2.0","html_url":"https://example.test/v1.2.0"}`,
			))
		},
	))
	defer server.Close()

	update, err := checkForUpdate(
		context.Background(),
		"v1.2.0",
		t.TempDir(),
		server.URL,
		server.Client(),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("unexpected version check error: %s", err)
	}
	if update != nil {
		t.Fatalf("did not expect an update notice: %#v", update)
	}
}

// TestCheckForUpdateReturnsFetchError checks that a network failure while
// fetching the latest release is surfaced to the caller as an error.
func TestCheckForUpdateReturnsFetchError(t *testing.T) {
	expectedErr := errors.New("offline")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, expectedErr
		}),
	}

	_, err := checkForUpdate(
		context.Background(),
		"v1.0.0",
		t.TempDir(),
		"https://example.test/latest",
		client,
		time.Now(),
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected fetch error, got: %v", err)
	}
}

// TestCheckForUpdateSkipsDevelopmentBuild checks that the version check is
// skipped entirely (no error, no update) when Version is empty, as it is for
// non-tagged/development builds.
func TestCheckForUpdateSkipsDevelopmentBuild(t *testing.T) {
	originalVersion := Version
	Version = ""
	t.Cleanup(func() {
		Version = originalVersion
	})

	update, err := CheckForUpdate(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected development build error: %s", err)
	}
	if update != nil {
		t.Fatalf("development build should not return an update: %#v", update)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
