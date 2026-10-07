package pkgmgr

import (
	"bytes"
	"context"
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
		"--cert-identity-regex",
		`^https://github\.com/blinklabs-io/actions/`,
	)
	return cmd.CombinedOutput()
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
	if bytes.Contains(output, []byte("No attestations found with predicate type:")) {
		return nil
	}
	if err != nil {
		return fmt.Errorf(
			"failed to verify GitHub release attestation for %s: %w: %s",
			repository,
			err,
			strings.TrimSpace(string(output)),
		)
	}
	if !bytes.Contains(output, []byte("Verification succeeded!")) {
		return errors.New("GitHub release attestation verification returned no verified result")
	}
	return nil
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
