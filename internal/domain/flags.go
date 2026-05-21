package domain

// shortToLong maps user-friendly short-form flag keys stored in Profile.Args
// to the canonical long-form llama-server accepts as `--<long>`. The UI
// editor stores keys like "ngl" / "ctx-size" because they read better; but
// llama-server only accepts the short form via the single-dash variant
// (`-ngl`), not `--ngl`. Translating here keeps existing profiles on disk
// working without a migration.
var shortToLong = map[string]string{
	"ngl": "n-gpu-layers",
	"c":   "ctx-size",
	"b":   "batch-size",
	"ub":  "ubatch-size",
	"fa":  "flash-attn",
	"t":   "threads",
	"np":  "parallel",
	"ctk": "cache-type-k",
	"ctv": "cache-type-v",
	"sm":  "split-mode",
	"ts":  "tensor-split",
}

// CanonicalFlag returns the long-form name for a Profile.Args key.
func CanonicalFlag(key string) string {
	if long, ok := shortToLong[key]; ok {
		return long
	}
	return key
}
