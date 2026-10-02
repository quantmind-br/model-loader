// Package procutil holds small process-related helpers shared across the
// service layer. It deliberately imports nothing from the service packages so
// it can be used by processmgr, downloadmgr, and proxysupervisor without
// creating an inter-service import cycle.
package procutil

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrStillAlive is returned by TerminateTree when the target is still alive
// after SIGKILL + the confirmation window (a process wedged in GPU/CUDA
// teardown). Callers must treat the backend as still resident (VRAM held).
var ErrStillAlive = errors.New("procutil: process still alive after SIGKILL")

// killConfirmGrace bounds the post-SIGKILL wait for the target to actually be
// reaped. GPU backends can hold VRAM for seconds while the driver tears down
// large allocations; returning before then lets callers purge state while the
// process still holds VRAM (P4/DF11).
const killConfirmGrace = 15 * time.Second

// Alive reports whether a process with the given PID exists and is signalable.
// It sends signal 0, which performs error checking without delivering a signal.
// EPERM (the process exists but is owned by another user) counts as alive.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

// StartTicks returns field 22 of /proc/<pid>/stat (process start time in
// clock ticks since boot). Together with the PID it uniquely identifies a
// process incarnation. Returns (0, err) when the process is gone or the
// file is unparseable. Linux-only, like recover.go.
func StartTicks(pid int) (uint64, error) {
	if pid <= 0 {
		return 0, errors.New("invalid pid")
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	// The comm field (field 2) is wrapped in parens and may itself contain
	// spaces or parens, so scan past the LAST ')' before splitting on spaces.
	s := string(data)
	rparen := strings.LastIndexByte(s, ')')
	if rparen < 0 || rparen+2 > len(s) {
		return 0, errors.New("procutil: malformed stat")
	}
	fields := strings.Fields(s[rparen+2:])
	// After the ')' the next field is (3) state; starttime is stat field 22,
	// i.e. index 19 in this post-comm slice (22 - 3).
	const starttimeIdx = 19
	if len(fields) <= starttimeIdx {
		return 0, errors.New("procutil: stat too short")
	}
	ticks, err := strconv.ParseUint(fields[starttimeIdx], 10, 64)
	if err != nil {
		return 0, err
	}
	return ticks, nil
}

// SameProcess reports whether pid is alive AND — when startTicks != 0 —
// its /proc starttime still matches, i.e. the PID was not recycled.
// startTicks == 0 degrades to Alive(pid) (legacy entries).
func SameProcess(pid int, startTicks uint64) bool {
	if !Alive(pid) {
		return false
	}
	if startTicks == 0 {
		return true
	}
	cur, err := StartTicks(pid)
	if err != nil {
		// The process is signalable (Alive) but /proc is unreadable
		// (permission, races). Do not falsely declare recycling.
		return true
	}
	return cur == startTicks
}

// TerminateTree terminates pid and its descendants: when pid leads its own
// process group (Setsid launches), signals -pid so the whole tree is swept;
// otherwise signals just pid. Sends SIGTERM, polls Alive(pid) every 100ms up
// to grace, then sends SIGKILL to the same target. When the target was the
// group (-pid), a final group SIGKILL is sent even if the leader exited
// gracefully, sweeping stragglers (ESRCH ignored). grace <= 0 skips SIGTERM
// and SIGKILLs immediately. Returns nil when the process is already gone;
// returns the SIGTERM/SIGKILL syscall error otherwise.
//
// After SIGKILL the target is CONFIRMED gone before returning: a process
// wedged in GPU/CUDA teardown stays Alive (and holds VRAM) for a while after
// the signal, so TerminateTree polls up to killConfirmGrace and returns
// ErrStillAlive if it never dies — callers must treat the backend as still
// resident and NOT purge its state (P4/DF11).
//
// The group SIGKILL sweep after leader death assumes no PGID reuse within the
// grace window — practically impossible at desktop fork rates.
func TerminateTree(pid int, grace time.Duration) error {
	if pid <= 0 {
		return nil
	}
	if !Alive(pid) {
		return nil
	}
	// Signal the whole group only when pid actually leads it (Setsid launches
	// satisfy pgid == pid). Foreground launches share the parent's group, so
	// they fall back to single-pid signaling.
	target := pid
	group := false
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		target = -pid
		group = true
	}
	killTarget := func(sig syscall.Signal) error {
		err := syscall.Kill(target, sig)
		if err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	if grace > 0 {
		if err := killTarget(syscall.SIGTERM); err != nil {
			return err
		}
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) {
			if !Alive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Escalate: SIGKILL the leader/target if still alive, then CONFIRM it is
	// actually gone before returning. SIGKILL cannot be ignored, but a process
	// in uninterruptible sleep (GPU/CUDA teardown) stays Alive — and holds VRAM
	// — for a while after the signal. Returning early lets Kill purge the
	// registry and the swap launch into contended VRAM → OOM (P4/DF11).
	aliveTarget := func() bool {
		if group {
			return groupAlive(pid)
		}
		return Alive(pid)
	}
	if err := killTarget(syscall.SIGKILL); err != nil {
		return err
	}
	deadline := time.Now().Add(killConfirmGrace)
	for aliveTarget() {
		if !time.Now().Before(deadline) {
			return ErrStillAlive
		}
		time.Sleep(50 * time.Millisecond)
	}

	return nil
}

// groupAlive excludes zombies: they retain a PID but have released GPU resources.
// A signalable group with unreadable proc state is conservatively still alive.
func groupAlive(pgid int) bool {
	if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return true
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return true
		}
		fields := strings.Fields(string(data)[end+1:])
		if len(fields) < 3 {
			return true
		}
		group, err := strconv.Atoi(fields[2])
		if err != nil {
			return true
		}
		if group == pgid && fields[0] != "Z" && fields[0] != "X" {
			return true
		}
	}
	return false
}
