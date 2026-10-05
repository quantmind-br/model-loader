# Troubleshooting

Common issues and their solutions.

## Backend binary not found

**Symptom:** Launch fails because no backend binary is available.

**Solution:**
- Check which backend the profile uses (`launch.backendId`) and verify its executable is on `PATH` or registered with an absolute path:
  ```bash
  model-loader backend probe
  which llama-server
  ```
- If the binary moved, update the backend executable via the Backends tab (`4`) edit flow (`UpdateBackend` preserves the id, kind, and schema reference). `backend add` rejects an already-registered name (`backend already exists`), so re-adding the same name is not an update path; changing the backend kind requires `backend delete` followed by `backend add` with the new `--kind`
- For `llama-server` kinds, ensure the build has server support

## Port already in use

**Symptom:** Starting the proxy fails with "port already in use".

**Solution:**
- Instance ports are assigned automatically by the process manager (a `port` argument in a profile is ignored and stripped), so backend port conflicts no longer occur
- If the proxy port (default `4321`) is taken, change `port` under `[serve]` in `config.toml` or pass `serve --port`
- Kill a stuck instance from the Server tab (`K`)

## Model file not found

**Symptom:** Launch fails with "model file does not exist".

**Solution:**
- Model existence is checked at profile create/edit time (`model-loader profile validate <id>` reports it before launch)
- Verify the `model` value in the profile editor; ensure the file exists and is readable
- `lmstudio` profiles use a daemon-side model key rather than a file path, and `vllm`, `sglang`, `unsloth`, and `freetoken` profiles may use a Hugging Face repo ID — those are not local paths and are exempt from the file check
- For local-file backends, update `search_paths` in `config.toml` to include the directory containing your models:
  ```toml
  [models]
  search_paths = ["~/models", "/path/to/your/models"]
  ```

## Background instance not recovered

**Symptom:** A background instance launched before closing the TUI does not appear in the Server tab on restart.

**Solution:**
- Check that `instances.json` exists in your state directory (`~/.local/state/model-loader/`)
- List running instances headlessly: `model-loader instance list`
- Verify the backend process is still running (for example `ps aux | grep -E 'llama-server|vllm|sglang'` matched to the profile's backend kind)
- If the process crashed, it will be marked with a crash indicator; clear it from the Server tab

## Foreground instance already running

**Symptom:** Launch fails with "a foreground instance is already running".

**Solution:**
- Only one foreground instance is allowed at a time
- Profiles launch as background (proxy-mediated) instances by default — the foreground limit only applies to an explicit foreground launch
- Kill the existing foreground instance from the Server tab (`K`)

## Health check timeout

**Symptom:** Launch succeeds but status shows "did not become healthy within timeout".

**Solution:**
- The instance may be taking longer than expected to load the model
- Check the logs in the Server tab for errors
- Verify the model is valid and compatible with the profile's backend version
- The headless `serve` path waits up to 360 seconds for slow model loads by default. If a very large model still exceeds it, raise the override under `[serve]` in `config.toml`:
  ```toml
  [serve]
  health_check_timeout_sec = 600
  ```
  `0` selects the caller default (360 seconds for headless `serve`); changes require a restart

## Model browser shows no files

**Symptom:** Models tab is empty.

**Solution:**
- The Models tab scans `search_paths` in `config.toml` for local `.gguf` files; it does not list remote Hugging Face repo IDs or `lmstudio` daemon-side keys
- Check that `search_paths` points to directories containing `.gguf` files
- Ensure the directories are readable
- Wait for the scan to complete; large directories may take a few seconds

## Validation errors when saving a profile

**Symptom:** Profile editor shows red validation errors.

**Solution:**
- Validation uses the schema of the selected backend. Each backend kind has its own validation schema source (see [backend-schema-update.md](backend-schema-update.md))
- Schemas are generated when you add a backend; live-help backends parse the binary's `--help`, curated and embedded backends use the checked-in flag catalog
- If a schema is missing or outdated, refresh it safely with `model-loader backend schema refresh <backend-id>` — this re-derives flag facts while keeping your `presentation` and `rules` customizations. Do not delete and re-add the backend (loses custom layout) and do not hand-edit the generated JSON (use `model-loader backend schema apply <backend-id> -f <file>` for intentional edits)
- Use the Advanced tab to enter flags not available in the Essentials tab

## VRAM still allocated after stopping inference

**Symptom:** GPU memory stays full after you stop sending requests, even though no chat is active.

**Solution:**
- VRAM is held by the running backend process (for example `llama-server`, `vllm`, or `sglang`), not by individual requests
- To fully release GPU memory, the backend has to exit. Either:
  - From the headless HTTP proxy: `curl -sX POST http://127.0.0.1:4321/_admin/unload` (use `?force=true` to skip waiting for in-flight requests)
  - Headlessly per instance: `model-loader instance stop <pid|id>`
  - From the TUI: Server tab → `K` to kill the selected instance
- Verify with `nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits` before and after
- The proxy does **not** call `nvidia-smi --gpu-reset` — that requires root and can disturb other GPU clients. Killing the process is the supported way

## Clipboard not working

**Symptom:** Copying model path from the Models tab does nothing.

**Solution:**
- The clipboard integration requires a display server (X11 or Wayland)
- On headless systems, clipboard operations are not available
- The model path is also shown in the UI detail panel for manual copying
