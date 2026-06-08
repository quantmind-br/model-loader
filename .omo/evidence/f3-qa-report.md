# F3 Manual QA Report — models-hf-search-download

**Verdict: REJECT**

**Date:** 2026-05-17
**Tester:** Sisyphus-Junior (F3)
**Binary tested:** `.sisyphus/evidence/model-loader-test` (commit `30d00b9`, freshly built)
**Mock server:** `.sisyphus/evidence/hfmock.go` listening on `localhost:9999`
**Mock isolation:** `HF_BASE_URL=http://localhost:9999`, `XDG_CONFIG_HOME=/tmp/ml-qa-test/config`, `XDG_STATE_HOME=/tmp/ml-qa-test/state`, `XDG_DATA_HOME=/tmp/ml-qa-test/data`, `search_paths = ['/tmp/ml-qa-test/models']`.

---

## Summary

The TUI happy-path was driven via tmux: `4` → `s` → `test` → `Enter` → `Space` → `Enter` → `q`. **Search overlay, GGUF tagging, file picker, and `.gguf` filtering all work as designed.** Download fails with HTTP 401 because the download URL is constructed against `hfhub.DefaultBaseURL` (hardcoded `https://huggingface.co`) and does NOT honor `HF_BASE_URL`. The mock never received a download request — the binary hit real Hugging Face instead.

Without a working `HF_BASE_URL` for downloads, the F3 plan's "verify file was saved to test path" step is unverifiable against a mock; the only way to exercise the download path would be against the public hub with a real public repo, which contradicts the F3 plan and the `hfhub.NewClient` contract.

---

## Acceptance Checklist (vs. F3 plan)

| Item | Result | Evidence |
|---|---|---|
| Search overlay appeared | PASS | `f3-step1-search.txt` |
| Results showed `[GGUF]` tag | PASS | `f3-step2b-typed.txt` |
| File picker showed `.gguf` files (filtered) | PASS | `f3-step3-filepicker.txt` |
| Download progress appeared | FAIL — terminal error flash instead | `f3-step4-download.txt` |
| File saved to test path | FAIL — `/tmp/ml-qa-test/models/` empty after run | `find /tmp/ml-qa-test/models -type f` returned nothing |

---

## Bug 1 — Download URL ignores `HF_BASE_URL` (BLOCKER)

**Location:** `internal/ui/pages/models.go:584`

```go
url := fmt.Sprintf("%s/%s/resolve/main/%s", hfhub.DefaultBaseURL, repoID, file)
```

`hfhub.DefaultBaseURL` is the const `"https://huggingface.co"` and is used directly here, while `hfhub.NewClient` reads `os.Getenv("HF_BASE_URL")` for search/repo-info. The download leg therefore always targets the public hub, which is:

1. **A bug for testability.** The plan (`task 1 / hfhub skeleton`) explicitly says `baseURL default; honra env HF_BASE_URL se setado — testes/mock` — meaning the entire client surface is supposed to be redirectable. Downloads currently break that contract.
2. **A surprise for users.** Any operator running model-loader behind a corporate HF mirror (`HF_BASE_URL=https://hub.example.com`) will be able to search but not download.

**Observed effect this run:** flash `download failed: download failed with status 401` (line in `f3-step4-download.txt`). Mock server log shows zero hits on `/resolve/main/...` (only search + repo_info hits were recorded).

**Suggested fix:** expose the resolved baseURL on `*hfhub.Client` (e.g. `(c *Client) BaseURL() string`) and have `startSelectedDownloads` use `p.hfClient.BaseURL()` rather than the const. Alternatively, have the client itself construct the download URL (it already does for `OpenDownload`) and expose a `(c *Client) DownloadURL(repoID, file string) string` helper.

---

## Step-by-Step Evidence

### Step 0 — Models tab
Saved: `.sisyphus/evidence/f3-step0-models-tab.txt`
Key lines:
```
 1 Launcher  │  2 Profiles  │  3 Monitor  │  4 Models  │  5 Backends  │  6 Server
Models
/tmp/ml-qa-test/models [0]
(no .gguf files in configured search paths — edit ~/.config/model-loader/config.toml or press [R] to rescan)
[1-6] tabs  [tab] next  [q] quit  [?] help | [/] filter  [R] rescan  [s] search HF  [enter] actions  [esc] clear
```
- Footer correctly advertises `[s] search HF`. PASS.

### Step 1 — Press `s`, search overlay
Saved: `.sisyphus/evidence/f3-step1-search.txt`
Key lines:
```
╭─...─╮
│ Search: _                                            │
│ [↑↓] move  [enter] select  [esc] close  [g] toggle GGUF-only │
╰─...─╯
```
- Overlay renders, hint row shows all expected bindings (`g` toggle). PASS.

### Step 2 — Type `test`
Saved: `.sisyphus/evidence/f3-step2b-typed.txt`
Note: `tmux send-keys -t hfqa "test"` (no `-l`) did NOT route the runes into the picker — captured as `f3-step2-typed.txt` (empty query). Using `tmux send-keys -l -t hfqa "t"` (literal flag) one rune at a time worked. This is a tmux-input quirk, not a TUI bug.
```
│ Search: test_                                                │
│ fake-org/test-gguf-model [GGUF]                              │
│ fake-org/test-safetensors-model                              │
│ [↑↓] move  [enter] select  [esc] close  [g] toggle GGUF-only │
```
- Both mock results rendered.
- `[GGUF]` suffix appears only on the result whose `tags` array contains `"gguf"`. PASS.
- Debounce confirmed via mock log — one HTTP request per keystroke, no duplicates after typing settled.

### Step 3 — Press `Enter` (select first result)
Saved: `.sisyphus/evidence/f3-step3-filepicker.txt`
```
Select .gguf files

> [ ] model-Q4_K_M.gguf (4.0 KB)
  [ ] model-Q8_0.gguf (8.0 KB)

space: toggle, enter: confirm, esc: cancel
```
- Picker correctly filters out `README.md` and `config.json` from the 4 siblings the mock returned.
- Sizes formatted (4.0 KB / 8.0 KB). PASS.

### Step 3b — Press Space (toggle file)
Saved: `.sisyphus/evidence/f3-step3b-selected.txt`
```
> [x] model-Q4_K_M.gguf (4.0 KB)
  [ ] model-Q8_0.gguf (8.0 KB)
```
- Selection toggle works. PASS.

### Step 4 — Press `Enter` (confirm download)
Saved: `.sisyphus/evidence/f3-step4-download.txt`
```
Models
/tmp/ml-qa-test/models [0]
(no .gguf files in configured search paths — edit ~/.config/model-loader/config.toml or press [R] to rescan)

download failed: download failed with status 401
```
- File picker closed (expected).
- **Expected:** download progress row in the footer + completed file under `/tmp/ml-qa-test/models/fake-org__test-gguf-model/` (snapshot path) or in `search_paths[0]` root.
- **Actual:** terminal-state failure flash. No `.partial` file, no destination file. FAIL.

### Mock server log (`hfmock.log`)
```
2026/05/17 14:53:29 hfmock listening on localhost:9999
2026/05/17 14:53:32 repo_info repo=fake-org/test-gguf-model     (← my pre-flight curl)
2026/05/17 14:54:44 search query="t"
2026/05/17 14:54:45 search query="te"
2026/05/17 14:54:45 search query="tes"
2026/05/17 14:54:45 search query="test"
2026/05/17 14:54:52 repo_info repo=fake-org/test-gguf-model     (← TUI after Enter on result)
```
No `download path=...` line — confirms the download request never reached the mock.

### Step 5 — `q` quit
TUI exited cleanly, shell prompt returned. PASS.

---

## Filesystem after run

```
$ find /tmp/ml-qa-test/models -type f
(empty)
```
No downloaded files, no `.partial` residue. The auto-rescan-on-completion path was therefore not exercised either.

---

## Bug 2 (minor / informational) — `tmux send-keys` without `-l`

Sending multi-character literals via `tmux send-keys -t hfqa "test"` did not deliver any rune to the bubbletea picker (capture showed `Search: _` even after the call). The `-l` (literal) flag is required: `tmux send-keys -t hfqa -l "t"` per character. This is a tmux/bubbletea integration quirk, not a model-loader bug — flagging it so future F3 runs/scripts use `-l`.

---

## Conclusion

**REJECT.** Search, debounce, GGUF tagging, repo info, file picker, `.gguf` filter, and selection all behave per spec. **The download phase is blocked by a real bug** (`internal/ui/pages/models.go:584`) that hardcodes the public HF base URL, defeating both the F3 mock strategy and any user-side `HF_BASE_URL` mirroring. Fix Bug 1 above, then re-run this same F3 plan — the remaining acceptance items should pass directly because every step that does not depend on the download URL already passed.
