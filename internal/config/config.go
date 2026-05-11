// Package config loads and persists the application TOML config.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// AppConfig is the in-memory representation of the user config.
type AppConfig struct {
	Paths  PathsConfig  `mapstructure:"paths"`
	Models ModelsConfig `mapstructure:"models"`
	UI     UIConfig     `mapstructure:"ui"`
}

type PathsConfig struct {
	ProfilesDir         string `mapstructure:"profiles_dir"`
	LogDir              string `mapstructure:"log_dir"`
	StateDir            string `mapstructure:"state_dir"`
	BackendsDir         string `mapstructure:"backends_dir"`
	LlamaServerBinaryPath string `mapstructure:"llama_server_binary_path"`
}

type ModelsConfig struct {
	SearchPaths []string `mapstructure:"search_paths"`
}

type UIConfig struct {
	DefaultTab  string `mapstructure:"default_tab"`
	Keybindings string `mapstructure:"keybindings"`
}

// DefaultConfigPath returns ~/.config/model-loader/config.toml.
func DefaultConfigPath() (string, error) {
	home, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(home, "model-loader", "config.toml"), nil
}

// Load reads the config from the default location, creating defaults if missing.
func Load() (AppConfig, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return AppConfig{}, err
	}
	return LoadFrom(path)
}

// LoadFrom reads the config from the given path. If the file does not exist,
// it is created with defaults.
func LoadFrom(path string) (AppConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")

	applyDefaults(v)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return AppConfig{}, fmt.Errorf("mkdir config dir: %w", err)
		}
		if err := v.SafeWriteConfigAs(path); err != nil {
			return AppConfig{}, fmt.Errorf("write default config: %w", err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return AppConfig{}, fmt.Errorf("read config: %w", err)
	}

	// Migrate old default_tab value after tab reorder (Profiles was tab 1, now Launcher is).
	if v.GetString("ui.default_tab") == "profiles" {
		v.Set("ui.default_tab", "launcher")
		if err := v.WriteConfigAs(path); err != nil {
			// Non-fatal: warn but continue with the corrected in-memory value.
			fmt.Fprintf(os.Stderr, "config migration warning: could not rewrite default_tab: %v\n", err)
		}
	}

	var cfg AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return AppConfig{}, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.Paths.ProfilesDir = expandTilde(cfg.Paths.ProfilesDir)
	cfg.Paths.LogDir = expandTilde(cfg.Paths.LogDir)
	cfg.Paths.StateDir = expandTilde(cfg.Paths.StateDir)
	cfg.Paths.BackendsDir = expandTilde(cfg.Paths.BackendsDir)
	cfg.Paths.LlamaServerBinaryPath = expandTilde(cfg.Paths.LlamaServerBinaryPath)
	for i, p := range cfg.Models.SearchPaths {
		cfg.Models.SearchPaths[i] = expandTilde(p)
	}
	return cfg, nil
}

// expandTilde replaces a leading "~" with the user's home directory.
func expandTilde(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

func applyDefaults(v *viper.Viper) {
	home, _ := os.UserHomeDir()
	v.SetDefault("paths.profiles_dir", filepath.Join(home, ".config", "model-loader", "profiles"))
	v.SetDefault("paths.log_dir", filepath.Join(home, ".local", "state", "model-loader", "logs"))
	v.SetDefault("paths.state_dir", filepath.Join(home, ".local", "state", "model-loader"))
	v.SetDefault("paths.backends_dir", filepath.Join(home, ".config", "model-loader", "backends"))
	v.SetDefault("models.search_paths", []string{
		filepath.Join(home, ".lmstudio", "models"),
		filepath.Join(home, "models"),
	})
	v.SetDefault("ui.default_tab", "launcher")
	v.SetDefault("ui.keybindings", "default")
}
