package pkgmgr

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

type commandErrorWithExitCode int

func (e commandErrorWithExitCode) Error() string { return "gh command failed" }

func (e commandErrorWithExitCode) ExitCode() int { return int(e) }

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGitHubReleaseRepository(t *testing.T) {
	testCases := []struct {
		url  string
		want string
	}{
		{
			url:  "https://github.com/blinklabs-io/cardano-up/releases/download/v1.2.3/cardano-up",
			want: "blinklabs-io/cardano-up",
		},
		{url: "https://example.com/releases/download/v1.2.3/cardano-up"},
		{url: "https://github.com/blinklabs-io/cardano-up/releases/tag/v1.2.3"},
		{url: "https://github.com/blinklabs-io/cardano-up"},
		{url: "https://github.com/--repo/attacker/releases/download/v1/app"},
	}
	for _, testCase := range testCases {
		if got := githubReleaseRepository(testCase.url); got != testCase.want {
			t.Errorf("githubReleaseRepository(%q) = %q, want %q", testCase.url, got, testCase.want)
		}
	}
}

func TestAttestationVerificationArgs(t *testing.T) {
	testCases := []struct {
		name       string
		repository string
		want       []string
	}{
		{
			name:       "Blink Labs release enforces trusted reusable workflow",
			repository: "blinklabs-io/cardano-up",
			want:       []string{"attestation", "verify", "artifact", "--repo", "blinklabs-io/cardano-up", "--signer-repo", "blinklabs-io/actions"},
		},
		{
			name:       "third-party release uses its own repository identity",
			repository: "example/project",
			want:       []string{"attestation", "verify", "artifact", "--repo", "example/project"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := attestationVerificationArgs("artifact", testCase.repository)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("attestationVerificationArgs() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestVerifyGitHubReleaseAttestation(t *testing.T) {
	original := runAttestationVerification
	t.Cleanup(func() { runAttestationVerification = original })

	const releaseURL = "https://github.com/blinklabs-io/cardano-up/releases/download/v1.2.3/cardano-up"
	const content = "release bytes"
	t.Run("verified artifact", func(t *testing.T) {
		runAttestationVerification = func(
			_ context.Context,
			artifactPath string,
			repository string,
		) ([]byte, error) {
			if repository != "blinklabs-io/cardano-up" {
				t.Fatalf("unexpected repository %q", repository)
			}
			got, err := os.ReadFile(artifactPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != content {
				t.Fatalf("unexpected artifact bytes %q", got)
			}
			return nil, nil
		}
		if err := verifyGitHubReleaseAttestation(context.Background(), releaseURL, []byte(content), slog.Default()); err != nil {
			t.Fatalf("unexpected verification error: %s", err)
		}
	})

	t.Run("no attestation published", func(t *testing.T) {
		runAttestationVerification = func(
			context.Context,
			string,
			string,
		) ([]byte, error) {
			return []byte("HTTP 404: Not Found"), errors.New("gh returned no attestations")
		}
		if err := verifyGitHubReleaseAttestation(context.Background(), releaseURL, []byte(content), slog.Default()); err != nil {
			t.Fatalf("missing optional attestation should not fail: %s", err)
		}
	})

	t.Run("gh unavailable", func(t *testing.T) {
		runAttestationVerification = func(context.Context, string, string) ([]byte, error) {
			return nil, &exec.Error{Name: "gh", Err: exec.ErrNotFound}
		}
		if err := verifyGitHubReleaseAttestation(context.Background(), releaseURL, []byte(content), slog.Default()); err != nil {
			t.Fatalf("missing optional gh CLI should not fail: %s", err)
		}
	})

	t.Run("gh authentication unavailable", func(t *testing.T) {
		runAttestationVerification = func(context.Context, string, string) ([]byte, error) {
			return nil, commandErrorWithExitCode(4)
		}
		if err := verifyGitHubReleaseAttestation(context.Background(), releaseURL, []byte(content), slog.Default()); err != nil {
			t.Fatalf("missing optional gh authentication should not fail: %s", err)
		}
	})

	t.Run("invalid attestation blocks install", func(t *testing.T) {
		runAttestationVerification = func(
			context.Context,
			string,
			string,
		) ([]byte, error) {
			return []byte("attestation signer did not match"), errors.New("verification failed")
		}
		if err := verifyGitHubReleaseAttestation(context.Background(), releaseURL, []byte(content), slog.Default()); err == nil {
			t.Fatal("expected invalid release attestation to block installation")
		}
	})

	t.Run("non-release URL skips GitHub lookup", func(t *testing.T) {
		runAttestationVerification = func(
			context.Context,
			string,
			string,
		) ([]byte, error) {
			t.Fatal("unexpected attestation lookup for non-release URL")
			return nil, nil
		}
		if err := verifyGitHubReleaseAttestation(context.Background(), "https://example.com/app", []byte(content), slog.Default()); err != nil {
			t.Fatalf("unexpected error for non-release URL: %s", err)
		}
	})
}

func TestPackageInstallVerifiesGitHubReleaseBeforeWriting(t *testing.T) {
	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("release artifact")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })

	originalVerifier := runAttestationVerification
	t.Cleanup(func() { runAttestationVerification = originalVerifier })

	t.Run("verified release is installed", func(t *testing.T) {
		runAttestationVerification = func(
			_ context.Context,
			artifactPath string,
			repository string,
		) ([]byte, error) {
			if repository != "blinklabs-io/cardano-up" {
				t.Fatalf("unexpected repository %q", repository)
			}
			if got, err := os.ReadFile(artifactPath); err != nil || string(got) != "release artifact" {
				t.Fatalf("attestation checked wrong artifact: %q, %v", got, err)
			}
			return nil, nil
		}
		cfg := newArchiveTestConfig(t)
		step := &PackageInstallStepFile{
			Filename: "cardano-up",
			Url:      "https://github.com/blinklabs-io/cardano-up/releases/download/v1.2.3/cardano-up",
		}
		if err := step.install(cfg, "cardano-up-1.2.3-default", ""); err != nil {
			t.Fatalf("install failed: %s", err)
		}
		if _, err := os.Stat(filepath.Join(cfg.DataDir, "cardano-up-1.2.3-default", "cardano-up")); err != nil {
			t.Fatalf("verified release was not installed: %s", err)
		}
	})

	t.Run("invalid release is not installed", func(t *testing.T) {
		runAttestationVerification = func(
			context.Context,
			string,
			string,
		) ([]byte, error) {
			return []byte("attestation signature mismatch"), errors.New("verification failed")
		}
		cfg := newArchiveTestConfig(t)
		step := &PackageInstallStepFile{
			Filename: "cardano-up",
			Url:      "https://github.com/blinklabs-io/cardano-up/releases/download/v1.2.3/cardano-up",
		}
		if err := step.install(cfg, "cardano-up-1.2.3-default", ""); err == nil {
			t.Fatal("expected invalid release attestation to fail installation")
		}
		if _, err := os.Stat(filepath.Join(cfg.DataDir, "cardano-up-1.2.3-default", "cardano-up")); !os.IsNotExist(err) {
			t.Fatalf("unverified release was written, stat error: %v", err)
		}
	})
}
