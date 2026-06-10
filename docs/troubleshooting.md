# Troubleshooting

Common issues and their solutions.

## llama-server not found

**Symptom:** Launch fails because no backend binary is available.

**Solution:**
- Ensure you have compiled [llama.cpp](https://github.com/ggerganov/llama.cpp) with server support
- Verify `llama-server` is in your `PATH`:
  ```bash
  which llama-server
  ```
- If installed in a custom location, register it as a backend in the TUI (Backends tab `4`, press `n`) or add it to your `PATH`

## Port already in use

**Symptom:** Starting the proxy fails with "port already in use".

**Solution:**
- Instance ports are assigned automatically by the process manager (a `port` argument in a profile is ignored and stripped), so backend port conflicts no longer occur
- If the proxy port (default `4321`) is taken, change `port` under `[serve]` in `config.toml` or pass `serve --port`
- Kill a stuck instance from the Server tab (`k`)

## Model file not found

**Symptom:** Launch fails with "model file not found".

**Solution:**
- Verify the model path in the profile editor
- Ensure the file exists and is readable
- Update `search_paths` in `config.toml` to include the directory containing your models:
  ```toml
  [models]
  search_paths = ["~/models", "/path/to/your/models"]
  ```

## Background instance not recovered

**Symptom:** A background instance launched before closing the TUI does not appear in the Server tab on restart.

**Solution:**
- Check that `instances.json` exists in your state directory (`~/.local/state/model-loader/`)
- Verify the process is still running: `ps aux | grep llama-server`
- If the process crashed, it will be marked with a crash indicator; clear it from the Server tab

## Foreground instance already running

**Symptom:** Launch fails with "a foreground instance is already running".

**Solution:**
- Only one foreground instance is allowed at a time
- Switch to background mode in the Profiles tab (`b`) before launching
- Or kill the existing foreground instance from the Server tab

## Health check timeout

**Symptom:** Launch succeeds but status shows "did not become healthy within timeout".

**Solution:**
- The instance may be taking longer than expected to load the model
- Check the logs in the Server tab for errors
- Verify the model file is valid and compatible with your llama-server version
- The health-check wait defaults to 180 seconds to accommodate slow model loads; if a very large model still exceeds it, the timeout is not currently configurable

## Model browser shows no files

**Symptom:** Models tab is empty.

**Solution:**
- Check that `search_paths` in `config.toml` points to directories containing `.gguf` files
- Ensure the directories are readable
- Wait for the scan to complete; large directories may take a few seconds

## Validation errors when saving a profile

**Symptom:** Profile editor shows red validation errors.

**Solution:**
- Validation uses the schema of the selected backend. Each backend has its own validation schema
- Schemas are auto-generated when you add a backend via the Backends tab (`n`) (by running the binary's `--help`)
- If a schema is missing or outdated, delete and re-add the backend, or manually edit the schema JSON in the backends directory
- Use the Advanced tab to enter flags not available in the Essentials tab

## VRAM still allocated after stopping inference

**Symptom:** GPU memory stays full after you stop sending requests, even though no chat is active.

**Solution:**
- VRAM is held by the running backend process (e.g. `llama-server`), not by individual requests
- To fully release GPU memory, the backend has to exit. Either:
  - From the headless HTTP proxy: `curl -sX POST http://127.0.0.1:4321/_admin/unload` (use `?force=true` to skip waiting for in-flight requests)
  - From the TUI: Server tab → `k` to kill the selected instance
- Verify with `nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits` before and after
- The proxy does **not** call `nvidia-smi --gpu-reset` — that requires root and can disturb other GPU clients. Killing the process is the supported way

## Clipboard not working

**Symptom:** Copying model path from the Models tab does nothing.

**Solution:**
- The clipboard integration requires a display server (X11 or Wayland)
- On headless systems, clipboard operations are not available
- The model path is also shown in the UI detail panel for manual copying
