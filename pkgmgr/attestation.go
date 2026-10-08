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
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/snappy"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	githubAttestationsAPI = "https://api.github.com/repos/"
	maxAttestationAPI     = 4 << 20
	maxAttestationBundle  = 32 << 20
	maxAttestationJSON    = 64 << 20
	maxAttestationCount   = 100
)

var attestationHTTPClient = &http.Client{Timeout: 30 * time.Second}

var newSigstoreTUFOptions = tuf.DefaultOptions

// This is the Sigstore TUF bootstrap root used by GitHub's attestation service.
//
//go:embed github-tuf-root.json
var githubTUFRoot []byte

type githubAttestationsResponse struct {
	Attestations []struct {
		BundleURL string `json:"bundle_url"`
	} `json:"attestations"`
}

var (
	githubVerifierMu sync.Mutex
	githubVerifier   *verify.Verifier
	publicVerifierMu sync.Mutex
	publicVerifier   *verify.Verifier
)

func githubReleaseRepository(releaseURL string) string {
	parsed, err := url.Parse(releaseURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) < 6 || parts[2] != "releases" || parts[3] != "download" || parts[4] == "" || parts[5] == "" {
		return ""
	}
	segment, err := url.PathUnescape(parts[0])
	if err != nil || !githubPathSegment.MatchString(segment) {
		return ""
	}
	repo, err := url.PathUnescape(parts[1])
	if err != nil || !githubPathSegment.MatchString(repo) {
		return ""
	}
	return segment + "/" + repo
}

var githubPathSegment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func verifyGitHubReleaseAttestation(ctx context.Context, releaseURL string, artifact []byte, cacheDir string, logger *slog.Logger) error {
	repository := githubReleaseRepository(releaseURL)
	if repository == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	parts := strings.SplitN(repository, "/", 2)
	digest := sha256.Sum256(artifact)
	endpoint := githubAttestationsAPI + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/attestations/sha256:" + hex.EncodeToString(digest[:])
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "cardano-up")
	response, err := attestationHTTPClient.Do(request)
	if err != nil {
		logger.Warn("could not check optional GitHub release attestation", "repository", repository, "error", err)
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		logger.Warn("GitHub release has no published attestation", "repository", repository)
		return nil
	}
	if response.StatusCode != http.StatusOK {
		logger.Warn("could not check optional GitHub release attestation", "repository", repository, "status", response.Status)
		return nil
	}
	body, err := readBounded(response.Body, maxAttestationAPI)
	if err != nil {
		return fmt.Errorf("read GitHub attestation response: %w", err)
	}
	var result githubAttestationsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode GitHub attestation response: %w", err)
	}
	if len(result.Attestations) == 0 {
		logger.Warn("GitHub release has no published attestation", "repository", repository)
		return nil
	}
	if len(result.Attestations) > maxAttestationCount {
		return fmt.Errorf("GitHub returned too many attestations for %s", repository)
	}
	var failures []error
	for _, attestation := range result.Attestations {
		bundleBytes, err := downloadAttestationBundle(ctx, attestation.BundleURL)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := verifyAttestationBundle(ctx, bundleBytes, repository, digest, filepath.Join(cacheDir, "sigstore")); err == nil {
			return nil
		} else {
			failures = append(failures, err)
		}
	}
	return fmt.Errorf("GitHub release has attestations, but none verified for %s: %w", repository, errors.Join(failures...))
}

func downloadAttestationBundle(ctx context.Context, bundleURL string) ([]byte, error) {
	parsed, err := url.Parse(bundleURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".blob.core.windows.net") {
		return nil, fmt.Errorf("reject unexpected GitHub attestation bundle URL %q", bundleURL)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	client := *attestationHTTPClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !strings.HasSuffix(strings.ToLower(req.URL.Hostname()), ".blob.core.windows.net") {
			return errors.New("attestation bundle redirect left GitHub's blob storage")
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download attestation bundle: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download attestation bundle: %s", response.Status)
	}
	return readBounded(response.Body, maxAttestationBundle)
}

func verifyAttestationBundle(ctx context.Context, data []byte, repository string, digest [sha256.Size]byte, cacheDir string) error {
	decodedLen, err := snappy.DecodedLen(data)
	if err != nil || decodedLen > maxAttestationJSON {
		return errors.New("invalid or oversized compressed attestation bundle")
	}
	decoded, err := snappy.Decode(nil, data)
	if err != nil {
		return fmt.Errorf("decompress attestation bundle: %w", err)
	}
	protobufBundle := &protobundle.Bundle{}
	if err := protojson.Unmarshal(decoded, protobufBundle); err != nil {
		return fmt.Errorf("decode attestation bundle: %w", err)
	}
	sigstoreBundle, err := bundle.NewBundle(protobufBundle)
	if err != nil {
		return fmt.Errorf("parse attestation bundle: %w", err)
	}
	content, err := sigstoreBundle.VerificationContent()
	if err != nil {
		return err
	}
	cert := content.Certificate()
	if cert == nil || len(cert.Issuer.Organization) == 0 {
		return errors.New("attestation bundle has no recognized signing certificate")
	}
	var verifier *verify.Verifier
	switch cert.Issuer.Organization[0] {
	case "GitHub, Inc.":
		verifier, err = githubSigstoreVerifier(ctx, cacheDir)
	case "sigstore.dev":
		verifier, err = publicSigstoreVerifier(ctx, cacheDir)
	default:
		return fmt.Errorf("untrusted attestation certificate issuer %q", cert.Issuer.Organization[0])
	}
	if err != nil {
		return fmt.Errorf("initialize Sigstore trust root: %w", err)
	}
	return verifyParsedAttestation(sigstoreBundle, repository, "sha256", digest[:], verifier)
}

func verifyParsedAttestation(sigstoreBundle *bundle.Bundle, repository, digestAlgorithm string, digest []byte, verifier *verify.Verifier) error {
	identity, err := attestationIdentity(repository)
	if err != nil {
		return err
	}
	policy := verify.NewPolicy(verify.WithArtifactDigest(digestAlgorithm, digest), verify.WithCertificateIdentity(identity))
	if _, err := verifier.Verify(sigstoreBundle, policy); err != nil {
		return fmt.Errorf("verify Sigstore attestation: %w", err)
	}
	signatureContent, err := sigstoreBundle.SignatureContent()
	if err != nil {
		return fmt.Errorf("read attestation signature content: %w", err)
	}
	if signatureContent.EnvelopeContent() == nil {
		return errors.New("attestation bundle does not contain an in-toto statement")
	}
	statement, err := signatureContent.EnvelopeContent().Statement()
	if err != nil {
		return fmt.Errorf("read attestation statement: %w", err)
	}
	if statement.GetPredicateType() != "https://slsa.dev/provenance/v1" {
		return fmt.Errorf("unexpected attestation predicate type %q", statement.GetPredicateType())
	}
	return nil
}

func attestationIdentity(repository string) (verify.CertificateIdentity, error) {
	parts := strings.SplitN(repository, "/", 2)
	sanPattern := `(?i)^https://github\.com/` + regexp.QuoteMeta(repository) + `/`
	if strings.HasPrefix(strings.ToLower(repository), "blinklabs-io/") {
		sanPattern = `(?i)^https://github\.com/blinklabs-io/actions/`
	}
	san, err := verify.NewSANMatcher("", sanPattern)
	if err != nil {
		return verify.CertificateIdentity{}, err
	}
	issuer, err := verify.NewIssuerMatcher("https://token.actions.githubusercontent.com", "")
	if err != nil {
		return verify.CertificateIdentity{}, err
	}
	return verify.CertificateIdentity{
		SubjectAlternativeName: san,
		Issuer:                 issuer,
		Extensions: certificate.Extensions{
			SourceRepositoryURI:      "https://github.com/" + repository,
			SourceRepositoryOwnerURI: "https://github.com/" + parts[0],
		},
	}, nil
}

func githubSigstoreVerifier(ctx context.Context, cacheDir string) (*verify.Verifier, error) {
	githubVerifierMu.Lock()
	defer githubVerifierMu.Unlock()
	if githubVerifier != nil {
		return githubVerifier, nil
	}
	opts := newSigstoreTUFOptions().WithRoot(githubTUFRoot).WithRepositoryBaseURL("https://tuf-repo.github.com").WithCachePath(cacheDir).WithContext(ctx)
	client, err := tuf.New(opts)
	if err != nil {
		return nil, err
	}
	trustedRootBytes, err := client.GetTarget("trusted_root.json")
	if err != nil {
		return nil, err
	}
	trustedRoot, err := root.NewTrustedRootFromJSON(trustedRootBytes)
	if err != nil {
		return nil, err
	}
	githubVerifier, err = verify.NewVerifier(trustedRoot, verify.WithSignedTimestamps(1))
	return githubVerifier, err
}

func publicSigstoreVerifier(ctx context.Context, cacheDir string) (*verify.Verifier, error) {
	publicVerifierMu.Lock()
	defer publicVerifierMu.Unlock()
	if publicVerifier != nil {
		return publicVerifier, nil
	}
	opts := newSigstoreTUFOptions().WithCachePath(cacheDir).WithContext(ctx)
	client, err := tuf.New(opts)
	if err != nil {
		return nil, err
	}
	trustedRoot, err := root.GetTrustedRoot(client)
	if err != nil {
		return nil, err
	}
	publicVerifier, err = verify.NewVerifier(trustedRoot,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	return publicVerifier, err
}

func readBounded(reader io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("response exceeds %d byte limit", max)
	}
	return data, nil
}
