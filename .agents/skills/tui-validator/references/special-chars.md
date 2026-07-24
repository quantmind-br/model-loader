# Special-character test set

Load this file when you reach **Phase 4 — Stress**.

The goal is to send each class through `scripts/tui-send.sh --literal` (or
`--paste-bracketed` for the paste tests), capture the screen, and verify that
what arrived in the TUI matches what was sent.

Each class lists what to send, what to expect, and the typical failure mode
to look out for.

## 1. Latin-extended (Portuguese, Spanish, French, German)

Send:

```
áéíóú ÁÉÍÓÚ
ãõ ÃÕ
çñü ÇÑÜ
àèìòù ÀÈÌÒÙ
âêîôû ÂÊÎÔÛ
ß æ œ ø
ção não coração
```

Expect: every glyph appears exactly once, in order, without combining-mark
drift. Typical bug: a TUI that reads input one byte at a time will display
multi-byte UTF-8 as two question marks (`??`) or two `▒` cells.

## 2. Symbols TUIs frequently mishandle

```
€ £ ¥ ₹ ₩
© ® ™ § ¶ † ‡
≈ ≠ ≤ ≥ ± × ÷
… — – ‹ › « » " " ' '
```

The em-dash and the curly quotes catch a lot of regex-based input filters.

## 3. Wide East-Asian characters

```
中文 测试 你好
日本語 こんにちは
한국어 안녕하세요
```

Each CJK glyph occupies **two** terminal columns. Watch for: the next column
displaying half of a box-drawing character (sign that the TUI counted bytes,
not display width).

## 4. Emoji (single, modified, ZWJ)

```
😀 🚀 ✨ 🔥          (single codepoint)
👍🏽 👍🏿            (Fitzpatrick skin-tone modifier)
👨‍👩‍👧‍👦              (ZWJ family)
🏳️‍🌈                (ZWJ + variation selector)
```

ZWJ sequences are the canary: a TUI that mishandles grapheme clusters will
render the family as 4 separate faces. Variation selectors often render as
extra "phantom" cells.

## 5. Combining marks (NFD-style input)

Send the base + combining form, then compare to the precomposed form:

```
NFD:  e + U+0301  → é
NFC:  é
```

Bash one-liner to send NFD form:

```bash
printf 'e\xCC\x81'   # e + COMBINING ACUTE ACCENT
```

A TUI that doesn't normalize will show `e` followed by a stray combining
character on its own. A TUI that does normalize should render identically to
the NFC input.

## 6. Control characters

In a non-input context (i.e., the TUI is showing a menu, not a text field),
send each of these and see what happens:

```
C-a C-b C-d C-e C-f C-g C-j C-k C-l C-n C-o C-p C-r C-s C-t C-u C-v C-w C-x C-y
```

Skip `C-c` (usually exit) and `C-z` (usually suspend) — those are tested in
Phase 3 as exit keys. Skip the danger list ones (`C-k`, `C-w`, `C-u`) unless
the user opted in.

A bound chord should produce a visible reaction. An unbound chord should
produce **nothing** (no garbled chars in the title bar, no escape-sequence
fragments leaking to the screen).

## 7. Bracketed paste

Send a multi-line block wrapped in `\e[200~ ... \e[201~`:

```
line one
line two with áé€中
line three
```

Use `scripts/tui-send.sh --paste-bracketed "..."`.

Expect: the TUI either treats the whole thing as a single insert (text field)
or rejects it as a unit (menu context). Failure mode: each character fires a
binding (`l`, `i`, `n`, `e`, …) and the TUI ends up in an unexpected modal
state.

## 8. Rapid input (key-roll)

Send a burst of 20 navigation keys with no delay:

```bash
tui-send.sh "$SESSION" Down Down Down Down Down Down Down Down Down Down Down Down Down Down Down Down Down Down Down Down
```

Expect: the TUI either coalesces them (one redraw at the end) or processes
each (20 distinct moves). Failure mode: it processes the first few and then
the input handler stalls — visible as a stuck cursor while keys are still
being consumed.

## 9. Mouse-mode noise (if mouse support is on)

If `tmux show-options -t "$session" -g mouse` is `on`, send some fake mouse
escape sequences and see if they corrupt the screen:

```bash
printf '\e[<0;10;5M\e[<0;10;5m' | tmux load-buffer -b mouse-test -
tmux paste-buffer -b mouse-test -t "$session" -d
```

A mouse-aware TUI should ignore them or react sensibly. A non-mouse TUI
should not start spitting garbage onto the screen.
