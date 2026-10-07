package pkgmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

var runAttestationVerification = func(
	ctx context.Context,
	artifactPath string,
	repository string,
) ([]byte, error) {
	cmd := exec.CommandContext(
		ctx,
		"gh",
		"attestation",
		"verify",
		artifactPath,
		"--repo",
		repository,
		"--signer-repo",
		"blinklabs-io/actions",
		"--format",
		"json",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		output = append(output, stderr.Bytes()...)
	}
	return output, err
}

func verifyGitHubReleaseAttestation(
	ctx context.Context,
	rawURL string,
	artifact []byte,
) error {
	repository := githubReleaseRepository(rawURL)
	if repository == "" {
		return nil
	}
	file, err := os.CreateTemp("", "cardano-up-release-*")
	if err != nil {
		return fmt.Errorf("failed to prepare release attestation verification: %w", err)
	}
	defer os.Remove(file.Name()) //nolint:errcheck
	if _, err := file.Write(artifact); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to prepare release attestation verification: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to prepare release attestation verification: %w", err)
	}

	output, err := runAttestationVerification(ctx, file.Name(), repository)
	if isMissingAttestationError(output) {
		return nil
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || isUnavailableGitHubAuth(output) {
			return nil
		}
		return fmt.Errorf(
			"failed to verify GitHub release attestation for %s: %w: %s",
			repository,
			err,
			strings.TrimSpace(string(output)),
		)
	}
	var verified []json.RawMessage
	if err := json.Unmarshal(output, &verified); err != nil {
		return fmt.Errorf("GitHub release attestation verification returned invalid JSON: %w", err)
	}
	if len(verified) == 0 {
		return nil
	}
	return nil
}

func isMissingAttestationError(output []byte) bool {
	return strings.Contains(strings.ToLower(string(output)), "no attestations found")
}

func isUnavailableGitHubAuth(output []byte) bool {
	message := strings.ToLower(string(output))
	return strings.Contains(message, "not logged into") ||
		strings.Contains(message, "authentication required") ||
		strings.Contains(message, "gh auth login")
}

func githubReleaseRepository(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 6 || parts[2] != "releases" || parts[3] != "download" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}
