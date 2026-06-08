# F3 Manual QA Re-run — models-hf-search-download

**Verdict: APPROVE**

**Date:** 2026-05-17
**Tester:** Sisyphus-Junior (F3)
**Binary tested:** `.sisyphus/evidence/model-loader-test` (rebuilt against HEAD `0924df1`)
**Mock server:** `.sisyphus/evidence/hfmock.go` listening on `localhost:9999` (User-Agent logging added on the download handler)
**Mock isolation:** `HF_BASE_URL=http://localhost:9999`, `XDG_CONFIG_HOME=/tmp/ml-qa-test/config`, `XDG_STATE_HOME=/tmp/ml-qa-test/state`, `XDG_DATA_HOME=/tmp/ml-qa-test/data`, `search_paths = ["/tmp/ml-qa-test/models"]`

---

## Summary

The previous F3 rejection (Bug 1 — download URL hardcoded `hfhub.DefaultBaseURL` so HF_BASE_URL was ignored) is **fixed**. Re-running the same tmux + hfmock happy path now shows:

- Download request reaches the mock at `localhost:9999` (NOT real `huggingface.co`).
- File `model-Q4_K_M.gguf` is written to `/tmp/ml-qa-test/models/`.
- Mock server log records the User-Agent header on the download request.
- TUI auto-rescans on completion and the file appears in the local table.
- Flash message confirms "downloaded: model-Q4_K_M.gguf".

All previously passing items (overlay, debounce, GGUF tag, file picker, `.gguf` filter, selection) still pass.

---

## Acceptance Checklist (vs. F3 plan)

| Item | Result | Evidence |
|---|---|---|
| Search overlay appeared | PASS | `f3-rerun-step1-search.txt` |
| Results showed `[GGUF]` tag | PASS | `f3-rerun-step2-typed.txt` |
| File picker showed `.gguf` files (filtered) | PASS | `f3-rerun-step3-filepicker.txt` |
| Download hit the MOCK (not real HF) | PASS | mock log: `download path=/fake-org/test-gguf-model/resolve/main/model-Q4_K_M.gguf` |
| Download progress / completion | PASS — flash "downloaded: model-Q4_K_M.gguf" | `f3-rerun-step4-download.txt` |
| File saved to test path | PASS — 208B file at `/tmp/ml-qa-test/models/model-Q4_K_M.gguf` | `find /tmp/ml-qa-test/models -type f` |
| User-Agent header sent on download | PASS — `model-loader/dev` | mock log entry |
| Auto-rescan after completion | PASS — table now shows `[1]` and `model-Q4_K_M.gguf` row | `f3-rerun-step4-download.txt` |

---

## Code-level confirmation of the fix

`internal/ui/pages/models.go:584` (HEAD):
```go
url := p.hfClient.DownloadURL(repoID, file)
```
`internal/service/hfhub/client.go`:
```go
func (c *Client) BaseURL() string { return c.baseURL }

// DownloadURL builds a direct download URL for a file in a repo.
func (c *Client) DownloadURL(repoID, filename string) string {
    return c.baseURL + "/" + repoID + "/resolve/main/" + filename
}
```
The client's `baseURL` is set from `HF_BASE_URL` in `NewClient` (`client.go:35-37`), so the entire client surface (search, repo_info, AND download) now honors the env var. Suggested fix from the previous report was applied verbatim — `c.BaseURL()` exposed and `models.go` calls `DownloadURL`.

---

## Step-by-Step Evidence

### Step 0 — Models tab
Saved: `f3-rerun-step0-models.txt`
```
Models
/tmp/ml-qa-test/models [0]
(no .gguf files in configured search paths — edit ~/.config/model-loader/config.toml or press [R] to rescan)
[1-6] tabs  [tab] next  [q] quit  [?] help | [/] filter  [R] rescan  [s] search HF  [enter] actions  [esc] clear
```

### Step 1 — Press `s` (search overlay)
Saved: `f3-rerun-step1-search.txt`
```
│ Search: _                                                                                                              │
│ [↑↓] move  [enter] select  [esc] close  [g] toggle GGUF-only                                                           │
```

### Step 2 — Type `test` (one rune per `tmux send-keys -l`)
Saved: `f3-rerun-step2-typed.txt`
```
│ Search: test_                                                                                                          │
│ fake-org/test-gguf-model [GGUF]                                                                                        │
│ fake-org/test-safetensors-model                                                                                        │
```
Mock log shows the debounced calls `query="t" "te" "tes" "test"`.

### Step 3 — Press `Enter` (file picker)
Saved: `f3-rerun-step3-filepicker.txt`
```
Select .gguf files

> [ ] model-Q4_K_M.gguf (4.00KB)
  [ ] model-Q8_0.gguf (8.00KB)

space: toggle, enter: confirm, esc: cancel
```
Picker filters out `README.md` and `config.json`, sizes formatted. PASS.

### Step 3b — Press `Space` (toggle)
Saved: `f3-rerun-step3b-selected.txt`
```
> [x] model-Q4_K_M.gguf (4.00KB)
  [ ] model-Q8_0.gguf (8.00KB)
```

### Step 4 — Press `Enter` (start download)
Saved: `f3-rerun-step4-download.txt`
```
Models
/tmp/ml-qa-test/models [1]
 Name                                  Size        Quant       Params    Path
 model-Q4_K_M.gguf                     208B        Q4_K_M                /tmp/ml-qa-test/models/model-Q4_K_M.gguf
...
downloaded: model-Q4_K_M.gguf
```
Counter changed from `[0]` → `[1]`; the table row appeared automatically (auto-rescan worked). PASS.

### Mock server log (`hfmock.log`)
```
2026/05/17 15:06:27 hfmock listening on localhost:9999
2026/05/17 15:07:14 search query="t"
2026/05/17 15:07:15 search query="te"
2026/05/17 15:07:15 search query="tes"
2026/05/17 15:07:15 search query="test"
2026/05/17 15:07:20 repo_info repo=fake-org/test-gguf-model
2026/05/17 15:07:29 download path=/fake-org/test-gguf-model/resolve/main/model-Q4_K_M.gguf user-agent="model-loader/dev"
```
**Critical**: a `download path=...` line exists, AT `localhost:9999`, with `user-agent="model-loader/dev"`. The previous run had NO such line.

### Filesystem after run
```
$ find /tmp/ml-qa-test/models -type f -ls
   859873      4 -rw-r--r--   1 diogo    diogo         208 mai 17 15:07 /tmp/ml-qa-test/models/model-Q4_K_M.gguf
```
Content (first ~200 bytes):
```
mock file content for /fake-org/test-gguf-model/resolve/main/model-Q4_K_M.gguf
xxxxxxxxxxxxxxxxxxxxxxxxx...
```
Matches the mock's body exactly — proves the bytes came from `localhost:9999`, not real Hugging Face.

---

## Diff vs. previous F3 report

| Aspect | Previous run | This run |
|---|---|---|
| Download flash | `download failed with status 401` | `downloaded: model-Q4_K_M.gguf` |
| Mock download hit | none | `/fake-org/test-gguf-model/resolve/main/model-Q4_K_M.gguf` |
| `/tmp/ml-qa-test/models` | empty | 1 file, 208 B |
| User-Agent header | n/a (no request) | `model-loader/dev` |

---

## Conclusion

**APPROVE.** Bug 1 is fixed at the right layer (`hfhub.Client.DownloadURL` honors `HF_BASE_URL`), the download path is fully redirectable, the TUI now downloads through the mock, writes the file to the configured search path, sets the documented User-Agent header, and auto-rescans the local table. All F3 acceptance criteria are satisfied.

Minor (unchanged, informational): `tmux send-keys` still requires `-l` per rune for the bubbletea picker — already noted in the previous report, not a model-loader bug.
