package pkgmgr_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/blinklabs-io/cardano-up/pkgmgr"
)

func TestNewDefaultConfig(t *testing.T) {
	testHome := "/path/to/user/home"
	var expectedCacheDir, expectedConfigDir string
	switch runtime.GOOS {
	case "linux":
		expectedCacheDir = filepath.Join(testHome, ".cache/cardano-up")
		expectedConfigDir = filepath.Join(testHome, ".config/cardano-up")
	case "darwin":
		expectedCacheDir = filepath.Join(testHome, "Library/Caches/cardano-up")
		expectedConfigDir = filepath.Join(testHome, "Library/Application Support/cardano-up")
	default:
		t.Fatalf("unsupported OS: %s", runtime.GOOS)
	}
	origEnvVars := setEnvVars(
		map[string]string{
			"HOME":            testHome,
			"XDG_CONFIG_HOME": "",
			"XDG_CACHE_HOME":  "",
		},
	)
	defer func() {
		setEnvVars(origEnvVars)
	}()
	cfg, err := pkgmgr.NewDefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if cfg.CacheDir != expectedCacheDir {
		t.Fatalf(
			"did not get expected cache dir, got %q, expected %q",
			cfg.CacheDir,
			expectedCacheDir,
		)
	}
	if cfg.ConfigDir != expectedConfigDir {
		t.Fatalf(
			"did not get expected config dir, got %q, expected %q",
			cfg.ConfigDir,
			expectedConfigDir,
		)
	}
}

func TestNewDefaultConfigXdgConfigCacheEnvVars(t *testing.T) {
	testHome := "/path/to/user/home"
	testXdgCacheHome := filepath.Join(testHome, ".cache-test")
	testXdgConfigHome := filepath.Join(testHome, ".config-test")
	var expectedCacheDir, expectedConfigDir string
	switch runtime.GOOS {
	case "linux":
		expectedCacheDir = filepath.Join(testXdgCacheHome, "cardano-up")
		expectedConfigDir = filepath.Join(testXdgConfigHome, "cardano-up")
	case "darwin":
		expectedCacheDir = filepath.Join(testHome, "Library/Caches/cardano-up")
		expectedConfigDir = filepath.Join(testHome, "Library/Application Support/cardano-up")
	default:
		t.Fatalf("unsupported OS: %s", runtime.GOOS)
	}
	origEnvVars := setEnvVars(
		map[string]string{
			"HOME":            testHome,
			"XDG_CONFIG_HOME": testXdgConfigHome,
			"XDG_CACHE_HOME":  testXdgCacheHome,
		},
	)
	defer func() {
		setEnvVars(origEnvVars)
	}()
	cfg, err := pkgmgr.NewDefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if cfg.CacheDir != expectedCacheDir {
		t.Fatalf(
			"did not get expected cache dir, got %q, expected %q",
			cfg.CacheDir,
			expectedCacheDir,
		)
	}
	if cfg.ConfigDir != expectedConfigDir {
		t.Fatalf(
			"did not get expected config dir, got %q, expected %q",
			cfg.ConfigDir,
			expectedConfigDir,
		)
	}
}

func TestNewDefaultConfigEmptyHome(t *testing.T) {
	expectedErrs := map[string]string{
		"linux":  "could not determine user config directory: neither $XDG_CONFIG_HOME nor $HOME are defined",
		"darwin": "could not determine user config directory: $HOME is not defined",
	}
	origEnvVars := setEnvVars(
		map[string]string{
			"HOME":            "",
			"XDG_CONFIG_HOME": "",
			"XDG_CACHE_HOME":  "",
		},
	)
	defer func() {
		setEnvVars(origEnvVars)
	}()
	_, err := pkgmgr.NewDefaultConfig()
	expectedErr, ok := expectedErrs[runtime.GOOS]
	if !ok {
		t.Fatalf("unsupported OS: %s", runtime.GOOS)
	}
	if err == nil || err.Error() != expectedErr {
		t.Fatalf(
			"did not get expected error, got %q, expected %q",
			err.Error(),
			expectedErr,
		)
	}
}

func setEnvVars(envVars map[string]string) map[string]string {
	origVars := map[string]string{}
	for k, v := range envVars {
		origVars[k] = os.Getenv(k)
		os.Setenv(k, v)
	}
	return origVars
}
