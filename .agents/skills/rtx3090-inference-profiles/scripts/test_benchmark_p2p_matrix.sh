#!/usr/bin/env bash
# Focused tests for benchmark-p2p-matrix.sh.
#
# Every test runs the runner against fake `model-loader`, `curl` and
# `nvidia-smi` binaries in a throwaway sandbox. Nothing here touches the live
# proxy, real profiles or a GPU: the runner reaches those only through the
# env-overridable command names exercised below.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUNNER="$HERE/benchmark-p2p-matrix.sh"

PASS=0
FAIL=0
fail() { printf 'FAIL: %s\n' "$1"; FAIL=$((FAIL + 1)); }
pass() { printf 'ok: %s\n' "$1"; PASS=$((PASS + 1)); }
assert_eq() { # want got msg
	if [ "$1" = "$2" ]; then pass "$3"; else fail "$3 (want [$1] got [$2])"; fi
}
assert_contains() { # haystack needle msg
	case "$1" in
	*"$2"*) pass "$3" ;;
	*) fail "$3 (missing [$2])" ;;
	esac
}
assert_not_contains() { # haystack needle msg
	case "$1" in
	*"$2"*) fail "$3 (unexpected [$2])" ;;
	*) pass "$3" ;;
	esac
}
assert_file() { # path msg
	if [ -f "$1" ]; then pass "$2"; else fail "$2 (missing file $1)"; fi
}

# make_sandbox <dir> — build fake bins + two one-knob-apart profiles.
# Fakes append their argv to $CALLLOG so tests can assert invocation/order.
make_sandbox() {
	local dir="$1"
	mkdir -p "$dir/bin" "$dir/profiles" "$dir/out"
	export CALLLOG="$dir/calls.log"
	: >"$CALLLOG"

	cat >"$dir/bin/fake-curl" <<'EOF'
#!/usr/bin/env bash
echo "curl $*" >>"$CALLLOG"
# emit a /_status body naming the initially-loaded profile
printf '{"running":true,"loaded_profile_id":"initial-loaded","loaded_pid":4242,"loaded_port":8080}\n'
EOF

	cat >"$dir/bin/fake-model-loader" <<'EOF'
#!/usr/bin/env bash
echo "model-loader $*" >>"$CALLLOG"
# `benchmark run` emits a full JSON result on stdout, log noise on stderr
case "$1 $2" in
"benchmark run")
	echo "loading profile through proxy..." >&2
	printf '{"schemaVersion":1,"mode":"llama-bench","presets":[{"name":"5%%/256","tokens_per_s":91.2},{"name":"90%%/128","tokens_per_s":88.1}]}\n'
	;;
"instance start")
	echo "restored" >&2
	;;
esac
EOF

	cat >"$dir/bin/fake-nvidia-smi" <<'EOF'
#!/usr/bin/env bash
echo "nvidia-smi $*" >>"$CALLLOG"
# index-first rows so the sampler can route by GPU (script emits its own header)
echo "0, 21000, 97, 288.10, 1695, 810, 60, 4, 8, Not Active"
echo "1, 20990, 96, 287.40, 1680, 810, 61, 4, 8, Not Active"
EOF

	chmod +x "$dir/bin/"fake-*

	# two profiles differing in exactly one leaf: args.tensor-split
	cat >"$dir/profiles/prof-p2p-a.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-a","name":"A","description":"variant A",
 "model":"/models/x.gguf",
 "args":{"split-mode":"tensor","tensor-split":"0.5,0.5","flash-attn":true},
 "extraArgs":["--cache-reuse","256"]}
EOF
	cat >"$dir/profiles/prof-p2p-b.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-b","name":"B","description":"variant B",
 "model":"/models/x.gguf",
 "args":{"split-mode":"tensor","tensor-split":"0.6,0.4","flash-attn":true},
 "extraArgs":["--cache-reuse","256"]}
EOF

	export MODEL_LOADER_BIN="$dir/bin/fake-model-loader"
	export CURL_BIN="$dir/bin/fake-curl"
	export NVIDIA_SMI_BIN="$dir/bin/fake-nvidia-smi"
	export MODEL_LOADER_PROFILES_DIR="$dir/profiles"
	export MODEL_LOADER_PROXY="http://127.0.0.1:59999"
	# fast interval so during-run sampling is observable in tests
	export P2P_MATRIX_TELEMETRY_INTERVAL="0.05"
}

run_matrix() { # extra args...; captures stdout+stderr, sets RC
	OUT="$("$RUNNER" "$@" 2>&1)"
	RC=$?
}

# ---------------------------------------------------------------------------
# 1. argument parsing: required flags, --runs default, bad values, unknown flag
# ---------------------------------------------------------------------------
test_arg_parsing() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"

	run_matrix --profile-b prof-p2p-b --label x --output-dir "$d/out"
	[ "$RC" -ne 0 ] && pass "missing --profile-a is an error" || fail "missing --profile-a should error"
	assert_contains "$OUT" "profile-a" "error names the missing flag"

	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label x \
		--output-dir "$d/out" --unknown-flag zzz
	[ "$RC" -ne 0 ] && pass "unknown flag rejected" || fail "unknown flag should error"

	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label x \
		--runs 0 --output-dir "$d/out"
	[ "$RC" -ne 0 ] && pass "--runs 0 rejected" || fail "--runs 0 should error"

	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label x \
		--runs abc --output-dir "$d/out"
	[ "$RC" -ne 0 ] && pass "non-numeric --runs rejected" || fail "non-numeric --runs should error"

	# default runs == 5 -> 5 measured A + 5 measured B = 10 benchmark runs
	make_sandbox "$d"
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label deflt \
		--output-dir "$d/out"
	assert_eq "0" "$RC" "default-runs invocation succeeds"
	local n
	n="$(grep -c 'model-loader benchmark run' "$CALLLOG")"
	# 10 measured + 2 warmup discards = 12
	assert_eq "12" "$n" "default 5 runs/variant -> 12 benchmark invocations (incl warmups)"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 2. A/B order: measured sequence alternates A,B,B,A,A,B,B,A,A,B
# ---------------------------------------------------------------------------
test_ab_order() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label ord \
		--runs 5 --output-dir "$d/out"
	assert_eq "0" "$RC" "order run succeeds"
	# collapse benchmark invocations to A/B letters; the first two are the
	# per-variant warmups (identical argv), so drop them.
	local seq
	seq="$(grep 'model-loader benchmark run' "$CALLLOG" \
		| tail -n +3 \
		| sed -e 's/.*--profile prof-p2p-a.*/A/' -e 's/.*--profile prof-p2p-b.*/B/' \
		| tr -d '\n')"
	assert_eq "ABBAABBAAB" "$seq" "measured order is balanced ABBAABBAAB"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 3. one-knob validation: >1 differing leaf is rejected, exactly 1 accepted
# ---------------------------------------------------------------------------
test_one_knob() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"

	# accepted: the sandbox profiles differ only in tensor-split
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label ok \
		--runs 1 --output-dir "$d/out"
	assert_eq "0" "$RC" "single-knob difference accepted"

	# rejected: mutate B to differ in a second leaf (flash-attn)
	cat >"$d/profiles/prof-p2p-b.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-b","name":"B","description":"variant B",
 "model":"/models/x.gguf",
 "args":{"split-mode":"tensor","tensor-split":"0.6,0.4","flash-attn":false},
 "extraArgs":["--cache-reuse","256"]}
EOF
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label bad \
		--runs 1 --output-dir "$d/out2"
	[ "$RC" -ne 0 ] && pass "two-knob difference rejected" || fail "two-knob difference should error"
	assert_contains "$OUT" "tensor-split" "diff report names differing knob 1"
	assert_contains "$OUT" "flash-attn" "diff report names differing knob 2"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 3b. one-knob validation over launch.env: a key/value entry is ONE knob, not
#     two (key + value). Canonicalized by key -> reported launch.env.<KEY>.
# ---------------------------------------------------------------------------
test_env_one_knob() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"
	# A: no NCCL_P2P_DISABLE. B: adds it. Same otherwise.
	cat >"$d/profiles/prof-p2p-a.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-a","name":"A","description":"env A",
 "model":"/models/x.gguf","args":{"tp":2},
 "launch":{"backendId":"vllm","env":[{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"}]}}
EOF
	cat >"$d/profiles/prof-p2p-b.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-b","name":"B","description":"env B",
 "model":"/models/x.gguf","args":{"tp":2},
 "launch":{"backendId":"vllm","env":[{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"},{"key":"NCCL_P2P_DISABLE","value":"1"}]}}
EOF
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label envadd \
		--runs 1 --output-dir "$d/out"
	assert_eq "0" "$RC" "adding one env key counts as a single knob (accepted)"

	# changing only an env VALUE is also one knob
	make_sandbox "$d"
	cat >"$d/profiles/prof-p2p-a.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-a","name":"A","description":"env A",
 "model":"/models/x.gguf","args":{"tp":2},
 "launch":{"env":[{"key":"NCCL_P2P_DISABLE","value":"1"}]}}
EOF
	cat >"$d/profiles/prof-p2p-b.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-b","name":"B","description":"env B",
 "model":"/models/x.gguf","args":{"tp":2},
 "launch":{"env":[{"key":"NCCL_P2P_DISABLE","value":"0"}]}}
EOF
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label envval \
		--runs 1 --output-dir "$d/out2"
	assert_eq "0" "$RC" "changing one env value counts as a single knob (accepted)"

	# two differing env keys -> rejected, reported as launch.env.<KEY>
	make_sandbox "$d"
	cat >"$d/profiles/prof-p2p-a.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-a","name":"A","description":"env A",
 "model":"/models/x.gguf","args":{"tp":2},"launch":{"env":[]}}
EOF
	cat >"$d/profiles/prof-p2p-b.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-b","name":"B","description":"env B",
 "model":"/models/x.gguf","args":{"tp":2},
 "launch":{"env":[{"key":"NCCL_P2P_DISABLE","value":"1"},{"key":"NCCL_DEBUG","value":"INFO"}]}}
EOF
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label env2 \
		--runs 1 --output-dir "$d/out3"
	[ "$RC" -ne 0 ] && pass "two differing env keys rejected" || fail "two env keys should error"
	assert_contains "$OUT" "launch.env.NCCL_P2P_DISABLE" "env diff reported by key 1"
	assert_contains "$OUT" "launch.env.NCCL_DEBUG" "env diff reported by key 2"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 3c. routing-identity args (args.served-model-name / args.model-name) are bound
#     to the profile id, so they never count as a knob. A + B differing in the
#     routing name AND one real knob is still ONE knob.
# ---------------------------------------------------------------------------
test_identity_arg_ignored() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"

	# A/B differ in served-model-name (== id) AND one real knob (tensor-split)
	cat >"$d/profiles/prof-p2p-a.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-a","name":"A","description":"variant A",
 "model":"/models/x.gguf",
 "args":{"split-mode":"tensor","tensor-split":"0.5,0.5","served-model-name":"prof-p2p-a"}}
EOF
	cat >"$d/profiles/prof-p2p-b.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-b","name":"B","description":"variant B",
 "model":"/models/x.gguf",
 "args":{"split-mode":"tensor","tensor-split":"0.6,0.4","served-model-name":"prof-p2p-b"}}
EOF
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label ident \
		--runs 1 --output-dir "$d/out"
	assert_eq "0" "$RC" "differing routing name + one real knob stays one knob (accepted)"

	# differing ONLY in the routing name -> zero real knobs -> rejected
	cat >"$d/profiles/prof-p2p-b.json" <<'EOF'
{"schemaVersion":3,"id":"prof-p2p-b","name":"B","description":"variant B",
 "model":"/models/x.gguf",
 "args":{"split-mode":"tensor","tensor-split":"0.5,0.5","served-model-name":"prof-p2p-b"}}
EOF
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label ident0 \
		--runs 1 --output-dir "$d/out2"
	[ "$RC" -ne 0 ] && pass "routing-name-only difference rejected (zero knobs)" || fail "zero real knobs should error"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 4. warmup discard: one warmup per variant, kept out of measured artifacts
# ---------------------------------------------------------------------------
test_warmup() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label warm \
		--runs 3 --output-dir "$d/out"
	# warmups are identical invocations distinguished only by artifact
	# placement: total benchmark runs = 2 warmup + 2*runs measured.
	local total
	total="$(grep -c 'model-loader benchmark run' "$CALLLOG")"
	assert_eq "8" "$total" "3 runs/variant -> 2 warmup + 6 measured = 8 invocations"
	# warmup artifacts live under warmup/, not among measured run files
	assert_file "$d/out/warmup/prof-p2p-a.json" "warmup A artifact retained separately"
	assert_file "$d/out/warmup/prof-p2p-b.json" "warmup B artifact retained separately"
	local measured
	measured="$(find "$d/out" -maxdepth 1 -name 'run-*.json' | wc -l | tr -d ' ')"
	assert_eq "6" "$measured" "3 runs/variant -> 6 measured JSON artifacts, warmups excluded"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 5. full artifact retention: JSON + log + telemetry per measured run + manifest
# ---------------------------------------------------------------------------
test_artifacts() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label keep \
		--runs 2 --output-dir "$d/out"
	assert_eq "0" "$RC" "artifact run succeeds"
	assert_file "$d/out/manifest.json" "manifest.json written"
	# first measured slot is A per ABBA
	assert_file "$d/out/run-01-prof-p2p-a.json" "run 01 JSON retained"
	assert_file "$d/out/run-01-prof-p2p-a.log" "run 01 log retained"
	assert_file "$d/out/run-01-prof-p2p-a.gpu0.csv" "run 01 GPU0 telemetry CSV retained"
	assert_file "$d/out/run-01-prof-p2p-a.gpu1.csv" "run 01 GPU1 telemetry CSV retained"
	# JSON artifact holds the FULL benchmark payload, not just an aggregate
	local body
	body="$(cat "$d/out/run-01-prof-p2p-a.json")"
	assert_contains "$body" "presets" "retained JSON is the full payload"
	# manifest records label, both profiles, and the initial loaded profile
	local man
	man="$(cat "$d/out/manifest.json")"
	assert_contains "$man" "keep" "manifest records label"
	assert_contains "$man" "initial-loaded" "manifest records initial loaded profile"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 6. telemetry: sampled DURING each run at an interval, per-GPU CSVs with the
#    full field set, and stopped after the run (incl. failure cleanup).
# ---------------------------------------------------------------------------
# helper: count how many data rows (index-first) a per-GPU CSV holds
csv_rows() { grep -cE '^[0-9]' "$1" 2>/dev/null || echo 0; }

test_telemetry() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"
	# make each benchmark run last long enough for several samples to land
	cat >"$d/bin/fake-model-loader" <<'EOF'
#!/usr/bin/env bash
echo "model-loader $*" >>"$CALLLOG"
case "$1 $2" in
"benchmark run") sleep 0.4; printf '{"presets":[{"tokens_per_s":90}]}\n' ;;
"instance start") echo "restored" >&2 ;;
esac
EOF
	chmod +x "$d/bin/fake-model-loader"

	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label tel \
		--runs 1 --output-dir "$d/out"
	assert_eq "0" "$RC" "telemetry run succeeds"

	# per-GPU CSVs exist for a measured run, each with a header naming the fields
	assert_file "$d/out/run-01-prof-p2p-a.gpu0.csv" "GPU0 CSV written"
	assert_file "$d/out/run-01-prof-p2p-a.gpu1.csv" "GPU1 CSV written"
	local hdr
	hdr="$(head -n1 "$d/out/run-01-prof-p2p-a.gpu0.csv")"
	for field in memory.used utilization.gpu power.draw clocks temperature.gpu \
		pcie.link.gen.current pcie.link.width.current; do
		assert_contains "$hdr" "$field" "GPU0 CSV header carries $field"
	done
	assert_contains "$hdr" "reasons" "GPU0 CSV header carries throttle reasons"

	# DURING-run interval sampling: with a 0.4s run and 0.05s interval there
	# must be several data rows per GPU, not a single snapshot.
	local r0 r1
	r0="$(csv_rows "$d/out/run-01-prof-p2p-a.gpu0.csv")"
	r1="$(csv_rows "$d/out/run-01-prof-p2p-a.gpu1.csv")"
	[ "$r0" -ge 2 ] && pass "GPU0 sampled repeatedly during run ($r0 rows)" \
		|| fail "GPU0 should have >=2 samples (got $r0)"
	[ "$r1" -ge 2 ] && pass "GPU1 sampled repeatedly during run ($r1 rows)" \
		|| fail "GPU1 should have >=2 samples (got $r1)"

	# sampler STOPPED after the run: call count must be stable once the script
	# returned (a leaked sampler would keep appending to the call log).
	local before after
	before="$(grep -c 'nvidia-smi ' "$CALLLOG")"
	sleep 0.3
	after="$(grep -c 'nvidia-smi ' "$CALLLOG")"
	assert_eq "$before" "$after" "telemetry sampler stopped after run (no leak)"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 6b. telemetry cleanup on failure: sampler still stops when a run crashes.
# ---------------------------------------------------------------------------
test_telemetry_failure_cleanup() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"
	cat >"$d/bin/fake-model-loader" <<'EOF'
#!/usr/bin/env bash
echo "model-loader $*" >>"$CALLLOG"
case "$1 $2" in
"benchmark run")
	n="$(grep -c 'benchmark run' "$CALLLOG")"
	sleep 0.2
	if [ "$n" -ge 3 ]; then echo "boom" >&2; exit 7; fi
	printf '{"warmup":true}\n'
	;;
"instance start") echo "restored" >&2 ;;
esac
EOF
	chmod +x "$d/bin/fake-model-loader"
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label telfail \
		--runs 2 --output-dir "$d/out2"
	[ "$RC" -ne 0 ] && pass "telemetry-failure run exits nonzero" || fail "should exit nonzero"
	# the crashing run still captured telemetry, and the sampler was stopped
	assert_file "$d/out2/run-01-prof-p2p-a.gpu0.csv" "crashing run still has GPU0 CSV"
	local before after
	before="$(grep -c 'nvidia-smi ' "$CALLLOG")"
	sleep 0.3
	after="$(grep -c 'nvidia-smi ' "$CALLLOG")"
	assert_eq "$before" "$after" "sampler stopped after failure (no leak)"
	rm -rf "$d"
}

# ---------------------------------------------------------------------------
# 7. trap restoration: initial profile restored on exit, even mid-run failure
# ---------------------------------------------------------------------------
test_trap_restore() {
	local d
	d="$(mktemp -d)"
	make_sandbox "$d"

	# happy path: restore runs once at the end via `instance start`
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label rst \
		--runs 1 --output-dir "$d/out"
	assert_eq "0" "$RC" "restore-path run succeeds"
	assert_contains "$(cat "$CALLLOG")" "instance start initial-loaded" \
		"initial profile restored via instance start"

	# failure path: crash on the first MEASURED run. Warmups are the first two
	# `benchmark run` invocations (identical argv), so fail on the 3rd onward.
	make_sandbox "$d"
	cat >"$d/bin/fake-model-loader" <<'EOF'
#!/usr/bin/env bash
echo "model-loader $*" >>"$CALLLOG"
case "$1 $2" in
"benchmark run")
	n="$(grep -c 'benchmark run' "$CALLLOG")"
	if [ "$n" -ge 3 ]; then echo "boom" >&2; exit 7; fi
	printf '{"warmup":true}\n'
	;;
"instance start") echo "restored" >&2 ;;
esac
EOF
	chmod +x "$d/bin/fake-model-loader"
	run_matrix --profile-a prof-p2p-a --profile-b prof-p2p-b --label fail \
		--runs 2 --output-dir "$d/out2"
	[ "$RC" -ne 0 ] && pass "mid-run failure propagates nonzero" || fail "failure should exit nonzero"
	assert_contains "$(cat "$CALLLOG")" "instance start initial-loaded" \
		"initial profile restored even after mid-run failure"
	rm -rf "$d"
}

test_arg_parsing
test_ab_order
test_one_knob
test_env_one_knob
test_identity_arg_ignored
test_warmup
test_artifacts
test_telemetry
test_telemetry_failure_cleanup
test_trap_restore

printf '\n=== benchmark-p2p-matrix: %d passed, %d failed ===\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
