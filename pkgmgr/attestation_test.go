package pkgmgr

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang/snappy"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"google.golang.org/protobuf/encoding/protojson"
)

type attestationRoundTripFunc func(*http.Request) (*http.Response, error)

func (f attestationRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGitHubReleaseRepository(t *testing.T) {
	tests := []struct {
		url, want string
	}{
		{"https://github.com/blinklabs-io/cardano-up/releases/download/v1.2.3/cardano-up", "blinklabs-io/cardano-up"},
		{"https://github.com/example/project/releases/download/v1/app", "example/project"},
		{"https://example.com/releases/download/v1/app", ""},
		{"https://github.com/blinklabs-io/cardano-up/releases/tag/v1.2.3", ""},
		{"https://user@github.com/blinklabs-io/cardano-up/releases/download/v1/app", ""},
		{"https://github.com/--repo/attacker/releases/download/v1/app", ""},
	}
	for _, test := range tests {
		if got := githubReleaseRepository(test.url); got != test.want {
			t.Errorf("githubReleaseRepository(%q) = %q, want %q", test.url, got, test.want)
		}
	}
}

func TestAttestationIdentity(t *testing.T) {
	tests := []struct {
		repository string
		wantSAN    string
	}{
		{"example/project", "https://github.com/example/project/.github/workflows/release.yml@refs/tags/v1"},
		{"blinklabs-io/cardano-up", "https://github.com/blinklabs-io/actions/.github/workflows/release.yml@refs/tags/v1"},
	}
	for _, test := range tests {
		identity, err := attestationIdentity(test.repository)
		if err != nil {
			t.Fatal(err)
		}
		if err := identity.SubjectAlternativeName.Verify(structCertificateSummary(test.wantSAN)); err != nil {
			t.Errorf("identity rejected expected signer SAN %q: %v", test.wantSAN, err)
		}
		if err := identity.SubjectAlternativeName.Verify(structCertificateSummary("https://github.com/untrusted/repo/workflow.yml@refs/tags/v1")); err == nil {
			t.Errorf("identity accepted unrelated signer SAN for %s", test.repository)
		}
	}
}

func structCertificateSummary(san string) certificate.Summary {
	return certificate.Summary{SubjectAlternativeName: san}
}

func TestVerifyGitHubReleaseAttestationLookup(t *testing.T) {
	oldClient := attestationHTTPClient
	t.Cleanup(func() { attestationHTTPClient = oldClient })
	const releaseURL = "https://github.com/example/project/releases/download/v1/app"
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{name: "missing optional attestation", statusCode: http.StatusNotFound},
		{name: "empty attestation list", statusCode: http.StatusOK, body: `{"attestations":[]}`},
		{name: "present malformed bundle fails closed", statusCode: http.StatusOK, body: `{"attestations":[{"bundle_url":"https://attestations.blob.core.windows.net/bundles/test"}]}`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			attestationHTTPClient = &http.Client{Transport: attestationRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.URL.Host == "attestations.blob.core.windows.net" {
					return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("not a compressed bundle")), Header: make(http.Header)}, nil
				}
				if request.URL.Host != "api.github.com" || !strings.Contains(request.URL.Path, "/attestations/sha256:") {
					t.Fatalf("unexpected GitHub API request %s", request.URL)
				}
				body := test.body
				return &http.Response{StatusCode: test.statusCode, Status: http.StatusText(test.statusCode), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			err := verifyGitHubReleaseAttestation(context.Background(), releaseURL, []byte("artifact"), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if (err != nil) != test.wantErr {
				t.Fatalf("verifyGitHubReleaseAttestation() error = %v, wantErr %v", err, test.wantErr)
			}
			wantRequests := 1
			if test.name == "present malformed bundle fails closed" {
				wantRequests = 2
			}
			if requests != wantRequests {
				t.Fatalf("got %d HTTP requests, want %d", requests, wantRequests)
			}
		})
	}
}

func TestDownloadAttestationBundleRejectsUntrustedURL(t *testing.T) {
	for _, rawURL := range []string{
		"http://attestations.blob.core.windows.net/bundle",
		"https://evil.example/bundle",
		"https://attestations.blob.core.windows.net.evil.example/bundle",
		"https://user@attestations.blob.core.windows.net/bundle",
	} {
		if _, err := downloadAttestationBundle(context.Background(), rawURL); err == nil {
			t.Errorf("downloadAttestationBundle(%q) unexpectedly succeeded", rawURL)
		}
	}
}

func TestAttestationIdentityIsCertificatePolicy(t *testing.T) {
	identity, err := attestationIdentity("example/project")
	if err != nil {
		t.Fatal(err)
	}
	config, err := verify.NewPolicy(verify.WithArtifactDigest("sha256", make([]byte, 32)), verify.WithCertificateIdentity(identity)).BuildConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !config.RequireIdentities() || !config.RequireArtifact() {
		t.Fatal("attestation policy must check signer identity and artifact digest")
	}
}

func TestVerifySignedSigstoreAttestation(t *testing.T) {
	fixtureDir := filepath.Join("testdata")
	bundleBytes, err := os.ReadFile(filepath.Join(fixtureDir, "sigstore-js-provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var apiBundle map[string]json.RawMessage
	if err := json.Unmarshal(bundleBytes, &apiBundle); err != nil {
		t.Fatal(err)
	}
	protobufJSON, err := json.Marshal(apiBundle)
	if err != nil {
		t.Fatal(err)
	}
	compressed := snappy.Encode(nil, protobufJSON)
	decoded, err := snappy.Decode(nil, compressed)
	if err != nil {
		t.Fatal(err)
	}
	protobufBundle := &protobundle.Bundle{}
	if err := protojson.Unmarshal(decoded, protobufBundle); err != nil {
		t.Fatalf("decode GitHub-format bundle fixture: %v", err)
	}
	sigstoreBundle, err := bundle.NewBundle(protobufBundle)
	if err != nil {
		t.Fatal(err)
	}
	content, err := sigstoreBundle.SignatureContent()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := content.EnvelopeContent().Statement()
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.GetSubject()) == 0 {
		t.Fatal("fixture has no attested subject")
	}
	digestHex := statement.GetSubject()[0].GetDigest()["sha512"]
	digestBytes, err := hex.DecodeString(digestHex)
	if err != nil || len(digestBytes) != 64 {
		t.Fatalf("fixture has invalid sha512 subject digest: %v", err)
	}

	rootBytes, err := os.ReadFile(filepath.Join(fixtureDir, "sigstore-public-good-root.json"))
	if err != nil {
		t.Fatal(err)
	}
	trustedRoot, err := root.NewTrustedRootFromJSON(rootBytes)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := verify.NewVerifier(trustedRoot,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyParsedAttestation(sigstoreBundle, "sigstore/sigstore-js", "sha512", digestBytes, verifier); err != nil {
		t.Fatalf("valid signed fixture did not verify: %v", err)
	}
	if err := verifyParsedAttestation(sigstoreBundle, "untrusted/project", "sha512", digestBytes, verifier); err == nil {
		t.Fatal("attestation from an unrelated repository unexpectedly verified")
	}
	if err := verifyParsedAttestation(sigstoreBundle, "sigstore/sigstore-js", "sha512", make([]byte, 64), verifier); err == nil {
		t.Fatal("attestation unexpectedly verified for a different artifact digest")
	}

}
