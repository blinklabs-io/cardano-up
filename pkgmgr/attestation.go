package pkgmgr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// #nosec G204 -- arguments are passed directly to gh without shell interpretation.
	cmd := exec.CommandContext(ctx, "gh", attestationVerificationArgs(artifactPath, repository)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		output = append(output, stderr.Bytes()...)
	}
	return output, err
}

func attestationVerificationArgs(artifactPath, repository string) []string {
	args := []string{"attestation", "verify", artifactPath, "--repo", repository}
	if strings.HasPrefix(repository, "blinklabs-io/") {
		args = append(args, "--signer-repo", "blinklabs-io/actions")
	}
	return args
}

func verifyGitHubReleaseAttestation(
	ctx context.Context,
	rawURL string,
	artifact []byte,
	logger *slog.Logger,
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
	if err != nil {
		if isMissingAttestationError(output) {
			return nil
		}
		if errors.Is(err, exec.ErrNotFound) {
			logger.Warn("skipping GitHub release attestation verification: gh is not installed")
			return nil
		}
		if commandExitCode(err) == 4 {
			logger.Warn("skipping GitHub release attestation verification: gh is not authenticated")
			return nil
		}
		return fmt.Errorf(
			"failed to verify GitHub release attestation for %s: %w: %s",
			repository,
			err,
			strings.TrimSpace(string(output)),
		)
	}
	return nil
}

func isMissingAttestationError(output []byte) bool {
	message := strings.ToLower(string(output))
	return strings.Contains(message, "no attestations found") ||
		strings.Contains(message, "404: not found") ||
		strings.Contains(message, "404 not found")
}

func commandExitCode(err error) int {
	var exitError interface{ ExitCode() int }
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
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
	if !validGitHubRepositoryPart(parts[0]) || !validGitHubRepositoryPart(parts[1]) {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func validGitHubRepositoryPart(part string) bool {
	if part == "" || part == "." || part == ".." || part[0] == '-' {
		return false
	}
	for _, char := range part {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}
