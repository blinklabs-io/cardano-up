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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	hashicorpversion "github.com/hashicorp/go-version"
)

const (
	latestReleaseURL = "https://api.github.com/repos/blinklabs-io/cardano-up/releases/latest"
	versionCacheFile = "latest_version.json"
	versionCacheTTL  = 24 * time.Hour
	// versionCheckTimeout bounds the worst case: main waits for this check to
	// finish before the process exits (see cmd/cardano-up/main.go), so on an
	// unresponsive endpoint this is the maximum added delay to a command
	// that would otherwise return faster. Kept well above real-world GitHub
	// API latency (well under 1s in practice) so a normal, working request
	// isn't cut short.
	versionCheckTimeout = 1500 * time.Millisecond
)

type Update struct {
	CurrentVersion string
	LatestVersion  string
	ReleaseURL     string
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckForUpdate returns release information when the running release is
// older than the latest published release. Development builds are skipped.
// httpClient makes the request; callers should normally pass
// http.DefaultClient. Taking it explicitly, rather than defaulting
// internally, gives callers an injection point for testing without needing
// to mutate the shared http.DefaultClient.Transport.
func CheckForUpdate(cacheDir string, httpClient *http.Client) (*Update, error) {
	if Version == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		versionCheckTimeout,
	)
	defer cancel()
	return checkForUpdate(
		ctx,
		Version,
		cacheDir,
		latestReleaseURL,
		httpClient,
		time.Now(),
	)
}

// checkForUpdate contains the actual update-check logic behind
// CheckForUpdate, with the current time and HTTP client passed in so tests
// can control caching and avoid real network calls.
func checkForUpdate(
	ctx context.Context,
	currentVersion string,
	cacheDir string,
	releaseURL string,
	httpClient *http.Client,
	now time.Time,
) (*Update, error) {
	current, err := hashicorpversion.NewVersion(currentVersion)
	if err != nil {
		return nil, fmt.Errorf("parse current version: %w", err)
	}

	release, latest, err := loadCachedRelease(cacheDir, now)
	if err != nil || release == nil {
		release, latest, err = fetchLatestRelease(ctx, releaseURL, httpClient)
		if err != nil {
			return nil, err
		}
		// Caching is best-effort: a read-only cache directory must not
		// suppress an otherwise valid update notice.
		_ = saveCachedRelease(cacheDir, *release)
	}

	if !latest.GreaterThan(current) {
		return nil, nil
	}
	return &Update{
		CurrentVersion: currentVersion,
		LatestVersion:  release.TagName,
		ReleaseURL:     release.HTMLURL,
	}, nil
}

// loadCachedRelease reads the cached release info from cacheDir. It returns
// a nil release (with no error) when the cached copy is older than
// versionCacheTTL, and an error when the file is missing, unreadable, or
// invalid, either of which signals the caller to fetch a fresh copy. The
// returned version is release.TagName already parsed by validateRelease, so
// callers never need to parse it a second time.
func loadCachedRelease(
	cacheDir string,
	now time.Time,
) (*releaseInfo, *hashicorpversion.Version, error) {
	cachePath := filepath.Join(cacheDir, versionCacheFile)
	stat, err := os.Stat(cachePath)
	if err != nil {
		return nil, nil, err
	}
	if stat.ModTime().Before(now.Add(-versionCacheTTL)) {
		return nil, nil, nil
	}
	content, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, nil, err
	}
	var release releaseInfo
	if err := json.Unmarshal(content, &release); err != nil {
		return nil, nil, err
	}
	parsed, err := validateRelease(release)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cached release: %w", err)
	}
	return &release, parsed, nil
}

// fetchLatestRelease calls the GitHub releases API and returns the parsed
// release info for the latest published release, along with release.TagName
// already parsed by validateRelease so callers never need to parse it a
// second time.
func fetchLatestRelease(
	ctx context.Context,
	releaseURL string,
	httpClient *http.Client,
) (*releaseInfo, *hashicorpversion.Version, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		releaseURL,
		nil,
	)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp == nil {
		return nil, nil, errors.New("empty latest release response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil, fmt.Errorf(
			"latest release request returned %s",
			resp.Status,
		)
	}
	var release releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, nil, err
	}
	parsed, err := validateRelease(release)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid latest release: %w", err)
	}
	return &release, parsed, nil
}

// validateRelease checks that a releaseInfo has the fields needed to build
// an Update, whether it came from the cache or a fresh API response, and
// returns TagName parsed as a version. A malformed tag is rejected here,
// before a fetched release is cached or a cached release is reused.
func validateRelease(release releaseInfo) (*hashicorpversion.Version, error) {
	if release.TagName == "" {
		return nil, errors.New("missing tag name")
	}
	if release.HTMLURL == "" {
		return nil, errors.New("missing release URL")
	}
	parsed, err := hashicorpversion.NewVersion(release.TagName)
	if err != nil {
		return nil, fmt.Errorf("invalid tag name %q: %w", release.TagName, err)
	}
	return parsed, nil
}

// saveCachedRelease writes release to cacheDir so the next check within
// versionCacheTTL can skip the network call. It writes to a temporary file
// and renames it into place, since the process runs this from a background
// goroutine that can be cut off by an os.Exit elsewhere at any moment — a
// direct write could otherwise leave a truncated cache file on disk.
func saveCachedRelease(cacheDir string, release releaseInfo) error {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	content, err := json.Marshal(release)
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(cacheDir, versionCacheFile+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(cacheDir, versionCacheFile)); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
