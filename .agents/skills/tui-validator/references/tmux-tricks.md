# tmux tricks for tui-validator

Quick reference for the tmux mechanics this skill relies on. Most of the
gotchas come down to: tmux's `send-keys` parses its arguments as **key
names**, not text, so anything with shell special meaning needs care.

## Sending keys vs sending text

`send-keys` takes one or more **key tokens**:

```bash
tmux send-keys -t "$s" "q"          # sends literal 'q'
tmux send-keys -t "$s" "Enter"      # sends Return
tmux send-keys -t "$s" "C-c"        # sends Ctrl-C
tmux send-keys -t "$s" "q" "Enter"  # sends 'q' then Return
```

For **arbitrary text in input widgets** (especially UTF-8), use
`tui-send.sh --literal`, which wraps `tmux send-keys -l`:

```bash
tui-send.sh "$s" --literal 'hello áé€中'
```

For **true paste behavior** (multi-line text areas, bracketed-paste testing),
use `load-buffer` + `paste-buffer` via `tui-send.sh --paste` or
`tui-send.sh --paste-bracketed`:

```bash
tui-send.sh "$s" --paste 'hello
world
áé€中'
```

## Bracketed-paste sequences

To make the TUI think a real paste happened (so it can switch to paste mode):

```bash
{ printf '\033[200~'; cat my-multiline-file; printf '\033[201~'; } \
  | tmux load-buffer -b bp -
tmux paste-buffer -b bp -t "$s" -d
```

`scripts/tui-send.sh --paste-bracketed` wraps this.

## Capturing the pane

```bash
tmux capture-pane -t "$s" -p              # plain text, only visible area
tmux capture-pane -t "$s" -p -e           # include SGR (colour) escape codes
tmux capture-pane -t "$s" -p -S -100      # also include 100 lines of scrollback
tmux capture-pane -t "$s" -p -J           # join wrapped lines into one
```

For TUIs, you almost always want **only the visible area** (no `-S`) because
the TUI repaints over scrollback. Use `-e` when you want to feed the output
to `aha` to render ANSI colours.

## Detecting a dead pane

```bash
tmux list-panes -t "$s" -F '#{pane_dead}'   # 1 if the process exited
tmux list-panes -t "$s" -F '#{pane_pid}'    # PID of the foreground process
```

When `pane_dead` is `1`, the TUI has exited. Capture the pane immediately
(the last frame is preserved) before killing the session.

## Resizing without an attached client

`refresh-client -C` lets you set the size of a session without a real client
attached:

```bash
tmux refresh-client -t "$s" -C "120x40"
```

This drives `SIGWINCH` to the TUI process, which should redraw at the new
size. Note: some old tmux versions silently ignore this if no client is
attached — if that happens, attach a hidden foot terminal with a fixed size
instead.

## Disabling tmux's own status line

When `status` is `on` (the default), tmux's status row eats one terminal row
that the TUI thinks it owns. Always turn it off in audits:

```bash
tmux set-option -t "$s" status off
```

(Already done by `tui-launch.sh`.)

## Read-only attach for screenshots

When attaching a real terminal just to take a `grim` screenshot, attach
read-only so accidental keypresses from the host machine don't leak into the
TUI:

```bash
tmux attach-session -t "$s" -r   # -r = read-only
```

## Cursor position

```bash
tmux display-message -t "$s" -p '#{cursor_x},#{cursor_y}'
```

Useful for detecting "cursor stuck" (R-03 in the failure-modes catalogue).

## Alternate-screen detection

```bash
tmux display-message -t "$s" -p '#{alternate_on}'  # 1 if alt-screen active
```

Most TUIs use the alternate screen buffer. If a TUI claims to be a TUI but
this flag is `0`, that's already a red flag — it'll trash the user's
scrollback when it exits.

## Useful one-liners

### List all tui-validator sessions
```bash
tmux list-sessions -F '#S' | grep '^tuiv-'
```

### Tail captures while an audit runs
```bash
watch -n 0.5 'tmux capture-pane -t SESSION -p | tail -20'
```

### Force-kill any stuck session
```bash
tmux kill-session -t SESSION
# or, nuclear:
scripts/tui-cleanup.sh --all
```
