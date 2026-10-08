package pkgmgr

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowedDownloadURL(t *testing.T) {
	testCases := []struct {
		url  string
		want bool
	}{
		{url: "https://example.com/file", want: true},
		{url: "http://localhost/file", want: true},
		{url: "http://service.localhost/file", want: true},
		{url: "http://127.0.0.1/file", want: true},
		{url: "http://10.0.0.1/file", want: true},
		{url: "http://172.16.0.1/file", want: true},
		{url: "http://172.31.255.254/file", want: true},
		{url: "http://192.168.1.10/file", want: true},
		{url: "http://[::1]/file", want: true},
		{url: "http://[fc00::1]/file", want: true},
		{url: "http://[fe80::1]/file", want: true},
		{url: "http://172.15.255.254/file", want: false},
		{url: "http://172.32.0.1/file", want: false},
		{url: "http://8.8.8.8/file", want: false},
		{url: "http://example.com/file", want: false},
		{url: "file:///tmp/file", want: false},
	}
	for _, testCase := range testCases {
		u, err := url.Parse(testCase.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := allowedDownloadURL(u); got != testCase.want {
			t.Errorf("allowedDownloadURL(%q) = %t, want %t", testCase.url, got, testCase.want)
		}
	}
}

func TestPackageInstallStepFileInstallHTTPFromLocalhost(t *testing.T) {
	const expectedContent = "local package artifact"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(expectedContent))
	}))
	defer srv.Close()

	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepFile{Filename: "artifact", Url: srv.URL + "/artifact"}
	if err := step.install(cfg, "test-1.0.0-default", ""); err != nil {
		t.Fatalf("local HTTP download failed: %s", err)
	}
	got, err := os.ReadFile(filepath.Join(cfg.DataDir, "test-1.0.0-default", "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != expectedContent {
		t.Fatalf("downloaded content = %q, want %q", got, expectedContent)
	}
}

func TestPackageInstallStepFileInstallHTTPRedirectPolicy(t *testing.T) {
	privateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("redirected artifact"))
	}))
	defer privateServer.Close()

	localRedirect := newArchiveTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, privateServer.URL, http.StatusFound)
	}))
	cfg := newArchiveTestConfig(t)
	step := &PackageInstallStepFile{Filename: "artifact", Url: localRedirect.URL}
	if err := step.install(cfg, "test-1.0.0-local", ""); err != nil {
		t.Fatalf("redirect to private HTTP host failed: %s", err)
	}

	publicRedirect := newArchiveTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/artifact", http.StatusFound)
	}))
	step.Url = publicRedirect.URL
	if err := step.install(cfg, "test-1.0.0-public", ""); err == nil || !strings.Contains(err.Error(), "private HTTP host") {
		t.Fatalf("expected public HTTP redirect rejection, got %v", err)
	}
}
