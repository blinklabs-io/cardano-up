package pkgmgr

import (
	"errors"
	"reflect"
	"runtime"
	"testing"
)

// Docker-tagged packages should be hidden when the daemon is unavailable.
func TestNewDefaultConfigRequiredPackageTagsWithoutDocker(t *testing.T) {
	origCheckDockerConnectivity := checkDockerConnectivity
	checkDockerConnectivity = func() error {
		return errors.New("docker unavailable")
	}
	defer func() {
		checkDockerConnectivity = origCheckDockerConnectivity
	}()
	setDefaultConfigTestEnv(t)

	cfg, err := NewDefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	expectedTags := []string{
		runtime.GOOS,
		runtime.GOARCH,
	}
	if !reflect.DeepEqual(cfg.RequiredPackageTags, expectedTags) {
		t.Fatalf(
			"did not get expected required package tags, got %#v, expected %#v",
			cfg.RequiredPackageTags,
			expectedTags,
		)
	}
}

// Docker-tagged packages should be available when the daemon is reachable.
func TestNewDefaultConfigRequiredPackageTagsWithDocker(t *testing.T) {
	origCheckDockerConnectivity := checkDockerConnectivity
	checkDockerConnectivity = func() error {
		return nil
	}
	defer func() {
		checkDockerConnectivity = origCheckDockerConnectivity
	}()
	setDefaultConfigTestEnv(t)

	cfg, err := NewDefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	expectedTags := []string{
		runtime.GOOS,
		runtime.GOARCH,
		"docker",
	}
	if !reflect.DeepEqual(cfg.RequiredPackageTags, expectedTags) {
		t.Fatalf(
			"did not get expected required package tags, got %#v, expected %#v",
			cfg.RequiredPackageTags,
			expectedTags,
		)
	}
}

func setDefaultConfigTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", "/path/to/user/home")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
}

// Docker-tagged packages should require Docker even when OS and arch tags match.
func TestPackageAvailableForTagsRequiresDockerTag(t *testing.T) {
	pkg := Package{
		Tags: []string{
			"docker",
			"darwin",
			"arm64",
		},
	}
	tags := []string{
		"darwin",
		"arm64",
	}

	if pkg.availableForTags(tags) {
		t.Fatal("expected docker-tagged package to be unavailable without docker tag")
	}
}

// Docker-tagged packages should be available when Docker, OS, and arch tags match.
func TestPackageAvailableForTagsWithDockerTag(t *testing.T) {
	pkg := Package{
		Tags: []string{
			"docker",
			"darwin",
			"arm64",
		},
	}
	tags := []string{
		"docker",
		"darwin",
		"arm64",
	}

	if !pkg.availableForTags(tags) {
		t.Fatal("expected docker-tagged package to be available with docker tag")
	}
}
