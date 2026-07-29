package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_CreatesDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}

	if !strings.HasSuffix(cfg.Paths.ProfilesDir, "profiles") {
		t.Errorf("default ProfilesDir should end with 'profiles', got %q", cfg.Paths.ProfilesDir)
	}
	if cfg.UI.DefaultTab != "launcher" {
		t.Errorf("default UI.DefaultTab = %q, want %q", cfg.UI.DefaultTab, "launcher")
	}
	if len(cfg.Models.SearchPaths) == 0 {
		t.Errorf("default SearchPaths must not be empty")
	}

	// File should have been written to disk.
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config file was not created at %s: %v", cfgPath, err)
	}
}

func TestLoad_MigratesOldProfilesDefaultTab(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	contents := `
[ui]
default_tab = "profiles"
`
	if err := os.WriteFile(cfgPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.UI.DefaultTab != "launcher" {
		t.Errorf("UI.DefaultTab = %q, want launcher (migrated from profiles)", cfg.UI.DefaultTab)
	}

	rewritten, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading rewritten config: %v", err)
	}
	if strings.Contains(string(rewritten), `default_tab = "profiles"`) {
		t.Errorf("config file still contains old default_tab = profiles; got:\n%s", rewritten)
	}
}

func TestLoad_RoundtripExisting(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	contents := `
[paths]
profiles_dir = "/tmp/p"
log_dir      = "/tmp/l"
state_dir    = "/tmp/s"

[models]
search_paths = ["/tmp/m"]

[ui]
default_tab = "monitor"
keybindings = "default"
`
	if err := os.WriteFile(cfgPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Paths.ProfilesDir != "/tmp/p" {
		t.Errorf("ProfilesDir = %q, want /tmp/p", cfg.Paths.ProfilesDir)
	}
	if cfg.UI.DefaultTab != "monitor" {
		t.Errorf("UI.DefaultTab = %q, want monitor", cfg.UI.DefaultTab)
	}
	if len(cfg.Models.SearchPaths) != 1 || cfg.Models.SearchPaths[0] != "/tmp/m" {
		t.Errorf("SearchPaths = %v, want [/tmp/m]", cfg.Models.SearchPaths)
	}
}

func TestLoad_DefaultLoggingLevelIsInfo(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadFrom(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("default logging.level = %q, want %q", cfg.Logging.Level, "info")
	}
}

// CFG1: the config file and every config-tree default must resolve against the
// same base. Before the fix DefaultConfigPath honoured $XDG_CONFIG_HOME while
// the paths.* defaults hardcoded $HOME/.config, so a redirected XDG split the
// operator's state across two trees with no error.
func TestConfigTreeDefaultsFollowConfigPathBase(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfgPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	wantBase := filepath.Join(xdg, "model-loader")
	if got := filepath.Dir(cfgPath); got != wantBase {
		t.Fatalf("config dir = %q, want %q", got, wantBase)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got, want := cfg.Paths.ProfilesDir, filepath.Join(wantBase, "profiles"); got != want {
		t.Errorf("ProfilesDir = %q, want %q", got, want)
	}
	if got, want := cfg.Paths.BackendsDir, filepath.Join(wantBase, "backends"); got != want {
		t.Errorf("BackendsDir = %q, want %q", got, want)
	}
	// The state tree is deliberately not XDG-config-scoped.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got, want := cfg.Paths.StateDir, filepath.Join(home, ".local", "state", "model-loader"); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}
