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
	Paths     PathsConfig     `mapstructure:"paths"`
	Models    ModelsConfig    `mapstructure:"models"`
	UI        UIConfig        `mapstructure:"ui"`
	Logging   LoggingConfig   `mapstructure:"logging"`
	Serve     ServeConfig     `mapstructure:"serve"`
	Benchmark BenchmarkConfig `mapstructure:"benchmark"`
}

// BenchmarkConfig controls the Benchmark tab's evaluation engine.
type BenchmarkConfig struct {
	MaxTokens         int              `mapstructure:"max_tokens"`          // generation cap per problem
	Temperature       float64          `mapstructure:"temperature"`         // sampling temperature
	TimeoutSec        int              `mapstructure:"timeout_sec"`         // per-problem inference timeout
	LongContextTokens int              `mapstructure:"long_context_tokens"` // target prompt size for needle probe (0 → 8000)
	SaveTranscripts   bool             `mapstructure:"save_transcripts"`    // capture raw model/judge I/O per run for debugging
	Judge             JudgeConfig      `mapstructure:"judge"`
	LlamaBench        LlamaBenchConfig `mapstructure:"llamabench"`
}

// LlamaBenchConfig tunes the throughput (llama-bench) scoring mode. Empty values
// fall back to engine defaults (presets 512/128 + 4096/256, 3 repetitions).
type LlamaBenchConfig struct {
	Presets     []string `mapstructure:"presets"`     // "pp/tg" pairs, e.g. ["512/128","4096/256"]
	Repetitions int      `mapstructure:"repetitions"` // measurements per preset; 0 → 3
	Warmup      int      `mapstructure:"warmup"`      // discarded warmup reps before measurement; <0 → 1; 0 disables
}

// JudgeConfig is the OpenAI-compatible endpoint used by the LLM-as-judge
// scoring mode. Left empty unless the user opts into judge mode.
type JudgeConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
	Model   string `mapstructure:"model"`
	// Samples is how many times the judge grades each problem; the run uses the
	// median score + majority resolved to reduce single-run noise.
	Samples int `mapstructure:"samples"`
}

// ServeConfig controls the headless HTTP proxy server exposed by
// `model-loader serve` and by the Server tab in the TUI.
type ServeConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// LoggingConfig controls the model-loader app logger (not the per-instance
// llama-server log file). Level values: "debug" | "info" | "warn" | "error".
// Unknown values fall back to "info" via log.ResolveLevel.
type LoggingConfig struct {
	Level string `mapstructure:"level"`
}

type PathsConfig struct {
	ProfilesDir           string `mapstructure:"profiles_dir"`
	LogDir                string `mapstructure:"log_dir"`
	StateDir              string `mapstructure:"state_dir"`
	BackendsDir           string `mapstructure:"backends_dir"`
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
	// Allow the judge endpoint to reference environment variables (e.g.
	// api_key = "$QUANTMIND_API_KEY") so secrets need not be written to disk.
	cfg.Benchmark.Judge.BaseURL = os.ExpandEnv(cfg.Benchmark.Judge.BaseURL)
	cfg.Benchmark.Judge.APIKey = os.ExpandEnv(cfg.Benchmark.Judge.APIKey)
	cfg.Benchmark.Judge.Model = os.ExpandEnv(cfg.Benchmark.Judge.Model)
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
	v.SetDefault("logging.level", "info")
	v.SetDefault("serve.host", "127.0.0.1")
	v.SetDefault("serve.port", 4321)
	v.SetDefault("benchmark.max_tokens", 32768)
	v.SetDefault("benchmark.temperature", 0.0)
	v.SetDefault("benchmark.timeout_sec", 120)
	v.SetDefault("benchmark.long_context_tokens", 0)
	v.SetDefault("benchmark.save_transcripts", true)
	v.SetDefault("benchmark.judge.base_url", "")
	v.SetDefault("benchmark.judge.api_key", "")
	v.SetDefault("benchmark.judge.model", "")
	v.SetDefault("benchmark.judge.samples", 3)
	v.SetDefault("benchmark.llamabench.presets", []string{"128/512", "512/128", "2048/256", "4096/256", "8192/128", "16384/64"})
	v.SetDefault("benchmark.llamabench.repetitions", 3)
	v.SetDefault("benchmark.llamabench.warmup", 1)
}
