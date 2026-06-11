package components

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

const (
	FlashLifetime      = 6 * time.Second
	FlashLifetimeError = 15 * time.Second
	FlashDimAfter      = 5 * time.Second

	// maxFlashItems caps the visible stack: a burst of messages keeps only
	// the newest three so the flash area never crowds out page content.
	maxFlashItems = 3
)

// FlashClearMsg is the tea.Msg emitted by the Cmd that Set returns, once
// the item's lifetime has elapsed. Tag identifies the owning page so
// unrelated Flash instances ignore it; Seq is the queue sequence number the
// item carried at scheduling so the clear pops exactly that item and
// nothing else (timestamps can collide when two messages land in the same
// Update cycle on a coarse clock). At is kept for display/debugging.
type FlashClearMsg struct {
	Tag string
	Seq uint64
	At  time.Time
}

// FlashItem is one queued status message with its severity and timestamp.
// Seq is the queue-unique identity its clear tick matches on.
type FlashItem struct {
	Message string
	Level   StatusLevel
	At      time.Time
	Seq     uint64
}

// Flash is a value-type auto-clearing status message queue bound to a page
// tag. Up to maxFlashItems messages stack (oldest first); each clears
// independently when its own lifetime tick arrives. Embed by value;
// mutation flows through the returned new Flash from Set/SetError and
// Update, mirroring the Confirm/Modal idiom in this package.
type Flash struct {
	tag   string
	seq   uint64 // last sequence number handed out; 0 = none yet
	items []FlashItem
}

// NewFlash builds a Flash bound to a page-identifying tag. The tag flows
// into every FlashClearMsg the Set Cmd emits so cross-page ticks are
// dropped by Update.
func NewFlash(tag string) Flash {
	return Flash{tag: tag}
}

// push appends a new item (copy-on-write: a stale Flash copy held elsewhere
// must never observe the append — bubbletea value-model hazard), evicts the
// oldest item beyond maxFlashItems, and returns a tea.Cmd that delivers
// FlashClearMsg for exactly this item after life elapses.
func (f Flash) push(msg string, level StatusLevel, life time.Duration) (Flash, tea.Cmd) {
	f.seq++
	item := FlashItem{Message: msg, Level: level, At: time.Now(), Seq: f.seq}
	items := make([]FlashItem, 0, len(f.items)+1)
	items = append(items, f.items...)
	items = append(items, item)
	if len(items) > maxFlashItems {
		items = items[len(items)-maxFlashItems:]
	}
	f.items = items
	tag := f.tag
	seq, at := item.Seq, item.At
	return f, tea.Tick(life, func(time.Time) tea.Msg {
		return FlashClearMsg{Tag: tag, Seq: seq, At: at}
	})
}

// Set queues an info-level message, stamps "now", and returns a tea.Cmd
// that delivers FlashClearMsg after FlashLifetime. Callers wire the cmd
// into their Update return so bubbletea drives the timer.
//
// Setting a new message before a previous tick fires is safe: each tick
// carries the Seq of the item it was scheduled for, so it pops only that
// item (or nothing, if the item was already evicted by overflow).
func (f Flash) Set(message string) (Flash, tea.Cmd) {
	return f.push(message, StatusInfo, FlashLifetime)
}

// SetError queues an error-level message with the longer error lifetime.
func (f Flash) SetError(message string) (Flash, tea.Cmd) {
	return f.push(message, StatusError, FlashLifetimeError)
}

// Update handles incoming FlashClearMsg. The returned bool is true iff an
// item was popped. Callers typically ignore the bool — they just thread
// the returned Flash back in:
//
//	case components.FlashClearMsg:
//	    p.flash, _ = p.flash.Update(msg)
//	    return p, nil
//
// Wrong-tag messages are silently dropped so multi-page apps using one
// Flash instance per page can all receive the global tick without
// trampling each other. A tick whose Seq matches no queued item (the item
// was evicted by overflow) is a no-op.
func (f Flash) Update(msg tea.Msg) (Flash, bool) {
	cm, ok := msg.(FlashClearMsg)
	if !ok {
		return f, false
	}
	if cm.Tag != f.tag {
		return f, false
	}
	for i, it := range f.items {
		if it.Seq == cm.Seq {
			// Copy-on-write removal: stale copies keep the old slice.
			items := make([]FlashItem, 0, len(f.items)-1)
			items = append(items, f.items[:i]...)
			items = append(items, f.items[i+1:]...)
			if len(items) == 0 {
				items = nil
			}
			f.items = items
			return f, true
		}
	}
	return f, false
}

// View renders the queued items oldest-first (most recent last), one line
// each. Error items use theme.Error, warnings theme.Warn, the rest
// theme.Subtitle; items older than FlashDimAfter render Faint so flashes
// visibly age before they auto-clear. Empty string when no flash is active
// so callers can use the result directly in lipgloss.JoinVertical without
// special-casing the empty state.
func (f Flash) View() string {
	if len(f.items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(f.items))
	for _, it := range f.items {
		style := theme.Subtitle
		switch it.Level {
		case StatusError:
			style = theme.Error
		case StatusWarn:
			style = theme.Warn
		}
		if !it.At.IsZero() && time.Since(it.At) >= FlashDimAfter {
			style = style.Faint(true)
		}
		lines = append(lines, style.Render(it.Message))
	}
	return strings.Join(lines, "\n")
}

// Message returns the most recent item's raw text (without styling).
// Empty string when no flash is active. Used by tests and pages that need
// to compose the flash into a custom render pipeline.
func (f Flash) Message() string {
	if len(f.items) == 0 {
		return ""
	}
	return f.items[len(f.items)-1].Message
}

// At returns the time the most recent item was Set. Zero value when no
// flash is active. Exposed for tests and dim-style detection by callers
// that render the message themselves rather than via View().
func (f Flash) At() time.Time {
	if len(f.items) == 0 {
		return time.Time{}
	}
	return f.items[len(f.items)-1].At
}

// Items returns a copy of the queued items, oldest first. Mutating the
// returned slice never affects the Flash.
func (f Flash) Items() []FlashItem {
	if len(f.items) == 0 {
		return nil
	}
	out := make([]FlashItem, len(f.items))
	copy(out, f.items)
	return out
}

// Current returns the message and level of the highest-severity queued
// item — the newest wins severity ties — so status bars surface an error
// even when an info message arrived after it. ("", StatusInfo) when empty.
func (f Flash) Current() (string, StatusLevel) {
	if len(f.items) == 0 {
		return "", StatusInfo
	}
	best := f.items[0]
	for _, it := range f.items[1:] {
		if it.Level >= best.Level {
			best = it
		}
	}
	return best.Message, best.Level
}

// Level returns the level Current would report: the highest severity among
// queued items, StatusInfo when empty.
func (f Flash) Level() StatusLevel {
	_, lvl := f.Current()
	return lvl
}
