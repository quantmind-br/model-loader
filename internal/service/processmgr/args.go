package processmgr

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// shortToLong maps user-friendly short-form flag keys stored in Profile.Args
// to the canonical long-form llama-server accepts as `--<long>`. The UI
// editor stores keys like "ngl" / "ctx-size" because they read better; but
// llama-server only accepts the short form via the single-dash variant
// (`-ngl`), not `--ngl`. Translating here keeps existing profiles on disk
// working without a migration.
var shortToLong = map[string]string{
	"ngl": "n-gpu-layers",
}

// canonicalFlag returns the long-form name for a Profile.Args key.
func canonicalFlag(key string) string {
	if long, ok := shortToLong[key]; ok {
		return long
	}
	return key
}

// BuildArgs converts a Profile into the CLI args slice used to spawn
// llama-server. The argument order is deterministic: --model first, then
// flags from p.Args sorted by key, then p.ExtraArgs verbatim.
//
// Value mapping:
//   - bool true  -> "--<key>"        (false is omitted; the flag's default
//     is assumed to be false; --no-X variants live in ExtraArgs)
//   - string     -> "--<key>" "<v>"
//   - float64    -> "--<key>" "<v>"  (printed as int if mathematically integral)
//   - []any      -> "--<key>" "<v0,v1,...>" (comma-joined, e.g. tensor-split)
func BuildArgs(p domain.Profile) []string {
	args := make([]string, 0, 2+2*len(p.Args)+len(p.ExtraArgs))
	args = append(args, "--model", p.Model)

	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		flag := "--" + canonicalFlag(k)
		switch v := p.Args[k].(type) {
		case bool:
			if v {
				args = append(args, flag)
			}
		case string:
			args = append(args, flag, v)
		case float64:
			args = append(args, flag, formatFloat(v))
		case []any:
			parts := make([]string, len(v))
			for i, x := range v {
				parts[i] = fmt.Sprint(x)
			}
			args = append(args, flag, strings.Join(parts, ","))
		}
	}
	args = append(args, p.ExtraArgs...)
	return args
}

func formatFloat(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
