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
	MaxTokens         int                 `mapstructure:"max_tokens"`          // generation cap per problem
	Limit             int                 `mapstructure:"limit"`               // cap items per reducible mode (0 → full set); a uniform reduced-run knob
	TimeoutSec        int                 `mapstructure:"timeout_sec"`         // per-problem inference timeout
	LongContextTokens int                 `mapstructure:"long_context_tokens"` // target prompt size for needle probe (0 → 8000)
	SaveTranscripts   bool                `mapstructure:"save_transcripts"`    // capture raw model/judge I/O per run for debugging
	UnloadAfterRun    bool                `mapstructure:"unload_after_run"`    // free the model (proxy /_admin/unload) when a run ends; default false keeps warm-model behavior
	Judge             JudgeConfig         `mapstructure:"judge"`
	LlamaBench        LlamaBenchConfig    `mapstructure:"llamabench"`
	Embeddings        EmbeddingsConfig    `mapstructure:"embeddings"`
	TerminalBench     TerminalBenchConfig `mapstructure:"terminalbench"`
	SweBenchPro       SweBenchProConfig   `mapstructure:"swebenchpro"`
	DeepSWE           DeepSWEConfig       `mapstructure:"deepswe"`
}

// SweBenchProConfig configures the agentic SWE-bench Pro scoring mode, which
// wraps the external harness (scaleapi/SWE-bench_Pro-os) + Docker + python. Empty
// scalar values fall back to the engine defaults shown below. The engine never
// installs the harness, Docker, or the patch-generation agent.
type SweBenchProConfig struct {
	HarnessDir    string   `mapstructure:"harness_dir"`     // cloned SWE-bench_Pro-os checkout (required for this mode)
	RawSamplePath string   `mapstructure:"raw_sample_path"` // --raw_sample_path; required, lowercase fail_to_pass/pass_to_pass columns (see docs/swe-bench-pro.md)
	ScriptsDir    string   `mapstructure:"scripts_dir"`     // --scripts_dir; empty → <harness>/run_scripts
	DockerhubUser string   `mapstructure:"dockerhub_user"`  // --dockerhub_username; empty → "jefzda"
	Python        string   `mapstructure:"python"`          // python interpreter; empty → "python3"
	NumWorkers    int      `mapstructure:"num_workers"`     // eval --num_workers; <=0 → 4 (single workstation)
	UseModal      bool     `mapstructure:"use_modal"`       // false → --use_local_docker; true → Modal cloud
	Instances     []string `mapstructure:"instances"`       // subset of instance_ids to evaluate; empty → all in the patch set
	SampleSeed    int      `mapstructure:"sample_seed"`     // seeds the deterministic --limit instance sampling
	PatchPath     string   `mapstructure:"patch_path"`      // pre-generated patches JSON or preds dir; takes precedence over agent_cmd; empty → require agent_cmd
	AgentCmd      []string `mapstructure:"agent_cmd"`       // patch-generation command ({model}/{api_base}/{output}/{instances}/{harness}); used only when patch_path is empty
	TimeoutSec    int      `mapstructure:"timeout_sec"`     // whole-pipeline cap (seconds); 0 → no model-loader-side cap
	ExtraArgs     []string `mapstructure:"extra_args"`      // passed through verbatim to swe_bench_pro_eval.py
}

// TerminalBenchConfig configures the agentic terminal-bench scoring mode, which
// wraps the external `tb` CLI (Terminal-Bench harness) + Docker. Empty scalar
// values fall back to the engine defaults shown below.
type TerminalBenchConfig struct {
	Command         string   `mapstructure:"command"`           // tb CLI binary (name on PATH or path); empty → "tb"
	Agent           string   `mapstructure:"agent"`             // tb agent; empty → "terminus"
	Dataset         string   `mapstructure:"dataset"`           // tb dataset 'name' or 'name==version'; empty → "terminal-bench-core==0.1.1"
	Provider        string   `mapstructure:"provider"`          // LiteLLM provider prefix for --model; empty → "openai"
	Tasks           []string `mapstructure:"tasks"`             // --task-id ids/globs; empty → whole dataset
	NTasks          int      `mapstructure:"n_tasks"`           // --n-tasks cap; 0 → omit
	SampleSeed      int      `mapstructure:"sample_seed"`       // seeds the deterministic --n-tasks→--task-id expansion (with n_tasks / --limit)
	Concurrent      int      `mapstructure:"concurrent"`        // --n-concurrent; <=0 → 1 (single-GPU rig)
	TimeoutSec      int      `mapstructure:"timeout_sec"`       // whole-run cap (seconds); 0 → no model-loader-side cap
	StallTimeoutSec int      `mapstructure:"stall_timeout_sec"` // group-kill tb when no new task is scored for this long (wedged agent/Docker); 0 → built-in default
	ExtraArgs       []string `mapstructure:"extra_args"`        // passed through verbatim (e.g. "--no-rebuild")
}

// DeepSWEConfig configures the agentic DeepSWE scoring mode, which wraps the
// external `pier` CLI (datacurve-ai/pier) running the DeepSWE task corpus
// (datacurve-ai/deep-swe) + Docker. Empty scalar values fall back to the engine
// defaults shown below. The engine never installs pier, Docker, or the corpus.
type DeepSWEConfig struct {
	Command         string   `mapstructure:"command"`           // pier CLI binary (name on PATH or path); empty → "pier"
	TasksDir        string   `mapstructure:"tasks_dir"`         // cloned deep-swe tasks/ dir (required for this mode)
	Agent           string   `mapstructure:"agent"`             // pier agent; empty → "mini-swe-agent"
	Provider        string   `mapstructure:"provider"`          // LiteLLM provider prefix for --model; empty → "openai"
	ModelClass      string   `mapstructure:"model_class"`       // mini-swe-agent model adapter; empty → "litellm" (chat completions)
	APIBase         string   `mapstructure:"api_base"`          // agent-facing api_base; empty → derived (<proxy>/v1, loopback→host.docker.internal). See docs/deep-swe.md
	Tasks           []string `mapstructure:"tasks"`             // --include-task-name ids/globs; empty → whole corpus
	NTasks          int      `mapstructure:"n_tasks"`           // --n-tasks cap; 0 → omit
	SampleSeed      int      `mapstructure:"sample_seed"`       // --sample-seed for deterministic subset (with n_tasks)
	Concurrent      int      `mapstructure:"concurrent"`        // --n-concurrent; <=0 → 1 (single-GPU rig)
	TimeoutSec      int      `mapstructure:"timeout_sec"`       // whole-run cap (seconds); 0 → no model-loader-side cap
	StallTimeoutSec int      `mapstructure:"stall_timeout_sec"` // group-kill pier when no new task is scored for this long (wedged agent/Docker); 0 → built-in default
	ExtraArgs       []string `mapstructure:"extra_args"`        // passed through verbatim (e.g. "--force-build")
}

// LlamaBenchConfig tunes the throughput (llama-bench) scoring mode. Empty values
// fall back to engine defaults (fill levels 5/25/50/90%, 3 repetitions).
type LlamaBenchConfig struct {
	// Presets are "<fill>%/<tg>" pairs, e.g. ["5%/256","25%/256","50%/256","90%/128"].
	// fill = percent of the profile's effective context to prefill with real content.
	// Keep in sync with benchmark.defaultPresets in llamabench_probe.go.
	Presets     []string `mapstructure:"presets"`
	Repetitions int      `mapstructure:"repetitions"` // measurements per preset; 0 → 3
	Warmup      int      `mapstructure:"warmup"`      // discarded warmup reps before measurement; <0 → 1; 0 disables
}

// EmbeddingsConfig optionally overrides where similarity graders fetch
// embeddings; empty reuses the model-under-test server.
type EmbeddingsConfig struct {
	BaseURL string `mapstructure:"base_url"`
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
	// HealthCheckTimeoutSec bounds how long the proxy waits for a backend's
	// /health after a swap before giving up. 0 selects the built-in default
	// (see cli/serve.go). Large models — e.g. a 35B int4 TP=2 multimodal MoE
	// whose Marlin expert repack alone runs minutes — can exceed the former
	// fixed 180s and get killed mid-boot.
	HealthCheckTimeoutSec int `mapstructure:"health_check_timeout_sec"`
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

// DefaultConfigPath returns <user-config-dir>/model-loader/config.toml, i.e.
// ~/.config/model-loader/config.toml unless $XDG_CONFIG_HOME redirects it.
func DefaultConfigPath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, "config.toml"), nil
}

// userConfigDir returns the model-loader config directory. Every config-tree
// default derives from this one base so the config file and the state it points
// at can never land in different trees: os.UserConfigDir honours
// $XDG_CONFIG_HOME, and hardcoding $HOME/.config for the paths.* defaults made
// a redirected XDG_CONFIG_HOME read config.toml from one tree while profiles and
// the backend catalog silently defaulted to the other (CFG1).
func userConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "model-loader"), nil
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
	cfg.Benchmark.SweBenchPro.HarnessDir = expandTilde(cfg.Benchmark.SweBenchPro.HarnessDir)
	cfg.Benchmark.SweBenchPro.RawSamplePath = expandTilde(cfg.Benchmark.SweBenchPro.RawSamplePath)
	cfg.Benchmark.SweBenchPro.ScriptsDir = expandTilde(cfg.Benchmark.SweBenchPro.ScriptsDir)
	cfg.Benchmark.SweBenchPro.PatchPath = expandTilde(cfg.Benchmark.SweBenchPro.PatchPath)
	cfg.Benchmark.DeepSWE.TasksDir = expandTilde(cfg.Benchmark.DeepSWE.TasksDir)
	return cfg, nil
}

// UpdateSearchPaths rewrites models.search_paths in the default config file,
// preserving every other value. Used by the Models tab to drop a broken search
// path without hand-editing config.toml. Comments and original key ordering are
// not preserved (viper re-serializes the whole file), matching the existing
// default_tab migration behavior in LoadFrom.
func UpdateSearchPaths(paths []string) error {
	path, err := DefaultConfigPath()
	if err != nil {
		return err
	}
	return updateSearchPathsAt(path, paths)
}

func updateSearchPathsAt(path string, paths []string) error {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	applyDefaults(v)
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	v.Set("models.search_paths", paths)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
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
	// Config-tree defaults follow DefaultConfigPath's base (CFG1); the state
	// tree stays under $HOME/.local/state.
	cfgDir, err := userConfigDir()
	if err != nil {
		cfgDir = filepath.Join(home, ".config", "model-loader")
	}
	v.SetDefault("paths.profiles_dir", filepath.Join(cfgDir, "profiles"))
	v.SetDefault("paths.log_dir", filepath.Join(home, ".local", "state", "model-loader", "logs"))
	v.SetDefault("paths.state_dir", filepath.Join(home, ".local", "state", "model-loader"))
	v.SetDefault("paths.backends_dir", filepath.Join(cfgDir, "backends"))
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
	v.SetDefault("benchmark.timeout_sec", 120)
	v.SetDefault("benchmark.long_context_tokens", 0)
	v.SetDefault("benchmark.save_transcripts", true)
	v.SetDefault("benchmark.judge.base_url", "")
	v.SetDefault("benchmark.judge.api_key", "")
	v.SetDefault("benchmark.judge.model", "")
	v.SetDefault("benchmark.judge.samples", 3)
	v.SetDefault("benchmark.embeddings.base_url", "")
	v.SetDefault("benchmark.llamabench.presets", []string{"5%/256", "25%/256", "50%/256", "90%/128"})
	v.SetDefault("benchmark.llamabench.repetitions", 3)
	v.SetDefault("benchmark.llamabench.warmup", 1)
	v.SetDefault("benchmark.terminalbench.command", "tb")
	v.SetDefault("benchmark.terminalbench.agent", "terminus")
	v.SetDefault("benchmark.terminalbench.dataset", "terminal-bench-core==0.1.1")
	v.SetDefault("benchmark.terminalbench.provider", "openai")
	v.SetDefault("benchmark.terminalbench.n_tasks", 0)
	v.SetDefault("benchmark.terminalbench.concurrent", 1)
	v.SetDefault("benchmark.terminalbench.timeout_sec", 0)
	v.SetDefault("benchmark.swebenchpro.dockerhub_user", "jefzda")
	v.SetDefault("benchmark.swebenchpro.python", "python3")
	v.SetDefault("benchmark.swebenchpro.num_workers", 4)
	v.SetDefault("benchmark.swebenchpro.use_modal", false)
	v.SetDefault("benchmark.swebenchpro.timeout_sec", 0)
}
