package backendschema

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// forkHelpFrom derives a fork-like --help from the real v10152 fixture by
// removing complete (wrapped) stanzas for the given upstream flags and
// appending a parsed-valid --kv-mean-center stanza. This mirrors
// llama.cpp-prisma-ml: a fork whose binary trails upstream (no CORS /
// reasoning-preserve / mtmd-batch-max-tokens) but adds one fork-only flag.
func forkHelpFrom(t *testing.T, drop []string) string {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/help-v10867.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	optStart := regexp.MustCompile(`^-`) // option lines begin with a dash at col 0
	dropSet := map[string]bool{}
	for _, d := range drop {
		dropSet[d] = true
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	skipping := false
	for _, ln := range lines {
		if optStart.MatchString(ln) {
			skipping = false
			for d := range dropSet {
				if regexp.MustCompile(`(^|[, ]+)--` + regexp.QuoteMeta(d) + `(?:[ ,]|$)`).MatchString(ln) {
					skipping = true
					break
				}
			}
		}
		if !skipping {
			out = append(out, ln)
		}
	}
	out = append(out,
		"--kv-mean-center FNAME                   path to a K-cache mean-centering bias file (GGUF)",
		"                                        (env: LLAMA_ARG_KV_MEAN_CENTER)",
	)
	return strings.Join(out, "\n")
}

// writeForkBinary writes a fake llama-server that emits the derived help and a
// fork version string.
func writeForkBinary(t *testing.T, help string) string {
	t.Helper()
	dir := t.TempDir()
	helpFile := filepath.Join(dir, "fork-help.txt")
	if err := os.WriteFile(helpFile, []byte(help), 0o644); err != nil {
		t.Fatalf("write help: %v", err)
	}
	bin := filepath.Join(dir, "llama-server")
	script := "#!/usr/bin/env bash\n" +
		"case \"$1\" in\n" +
		"  --help|--usage) cat " + helpFile + " ;;\n" +
		"  --version) echo 'version: 9597 (7529fdaaf)' ;;\n" +
		"  *) echo unknown 1>&2; exit 2 ;;\n" +
		"esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	return bin
}

// S8: a fork backend that live-parses keeps its parsed surface authoritative on
// flag existence — upstream-only curated flags must not be injected, while the
// fork's own kv-mean-center survives.
func TestLlamaGenerator_ForkLiveParseEnrichesOnly(t *testing.T) {
	phantom := []string{"cors-origins", "cors-methods", "cors-headers", "cors-credentials", "reasoning-preserve", "mtmd-batch-max-tokens"}
	help := forkHelpFrom(t, phantom)
	bin := writeForkBinary(t, help)

	store := backendcatalog.NewFSSchemaStore(t.TempDir())
	g := NewLlamaServerGenerator(store)
	backend := domain.Backend{
		ID:         "llama-fork",
		Kind:       domain.BackendKindLlamaServer,
		Executable: bin,
		SchemaRef:  "schemas/llama-fork.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(schema.Source.SourceVersion, "9597") {
		t.Errorf("SourceVersion = %q, want it to record the fork build 9597", schema.Source.SourceVersion)
	}
	for _, absent := range phantom {
		if _, ok := schema.Flags[absent]; ok {
			t.Errorf("upstream-only curated flag %q injected into fork schema; live parse must be authoritative", absent)
		}
	}
	if _, ok := schema.Flags["kv-mean-center"]; !ok {
		t.Error("fork-only parsed flag kv-mean-center missing from generated schema")
	}
	// Enrichment still applies to matched flags.
	if spec, ok := schema.Flags["ctx-size"]; !ok || spec.Group == "" {
		t.Error("ctx-size missing or not enriched with curated Group")
	}
	// Presentation must not reference flags the schema no longer defines.
	if schema.Presentation != nil {
		for _, g := range schema.Presentation.Groups {
			for _, long := range g.Flags {
				if _, ok := schema.Flags[long]; !ok {
					t.Errorf("presentation group %q references undefined flag %q", g.Name, long)
				}
			}
		}
	}
}
