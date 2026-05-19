package monitor

import (
	"regexp"
	"strconv"
	"sync"
	"time"
)

var tokensPerSecRe = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s+tokens\s+per\s+second`)

type tokenSample struct {
	at  time.Time
	val float64
}

type reqSample struct {
	at  time.Time
	val float64
}

// metricsAgg accumulates token and request counters that the three monitor
// pumps populate and that runMetricsTick periodically samples to emit
// Metrics events.
//
// Concurrency:
//   - mu guards every field below. Hold mu for every read and every write.
//   - observeLog(at, line):     called from runLogPump; appends to tokens.
//   - observeSlots(at, snap):   called from runSlotsPump; appends to requests,
//                               updates lastSlots and lastTime.
//   - snapshot(now):            called from runMetricsTick; reads tokens and
//                               requests after calling prune(now).
//   - prune(now):               called by observeLog, observeSlots, snapshot
//                               before doing their work; trims out-of-window
//                               samples from tokens and requests.
type metricsAgg struct {
	mu        sync.Mutex
	window    time.Duration
	tokens    []tokenSample   // appended by observeLog
	requests  []reqSample     // appended by observeSlots
	lastSlots map[int]int     // slotID → last NDecoded; written by observeSlots
	lastTime  time.Time       // written by observeSlots
}

func newMetricsAgg(window time.Duration) *metricsAgg {
	return &metricsAgg{
		window:    window,
		lastSlots: map[int]int{},
	}
}

// observeLog scans line for "X tokens per second" and stores X.
func (a *metricsAgg) observeLog(at time.Time, line string) {
	m := tokensPerSecRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.tokens = append(a.tokens, tokenSample{at: at, val: v})
	a.prune(at)
	a.mu.Unlock()
}

// observeSlots derives requests/s from the diff in summed n_decoded.
func (a *metricsAgg) observeSlots(at time.Time, snap SlotSnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()
	curSum := 0
	curMap := make(map[int]int, len(snap.Slots))
	for _, s := range snap.Slots {
		curSum += s.NDecoded
		curMap[s.ID] = s.NDecoded
	}
	if !a.lastTime.IsZero() {
		prevSum := 0
		for id, v := range a.lastSlots {
			if cur, ok := curMap[id]; ok && cur >= v {
				prevSum += v
			}
		}
		dt := at.Sub(a.lastTime).Seconds()
		if dt > 0 {
			rate := float64(curSum-prevSum) / dt
			if rate < 0 {
				rate = 0
			}
			a.requests = append(a.requests, reqSample{at: at, val: rate})
		}
	}
	a.lastSlots = curMap
	a.lastTime = at
	a.prune(at)
}

func (a *metricsAgg) prune(now time.Time) {
	cutoff := now.Add(-a.window)
	i := 0
	for i < len(a.tokens) && a.tokens[i].at.Before(cutoff) {
		i++
	}
	a.tokens = a.tokens[i:]
	j := 0
	for j < len(a.requests) && a.requests[j].at.Before(cutoff) {
		j++
	}
	a.requests = a.requests[j:]
}

func (a *metricsAgg) snapshot(now time.Time) Metrics {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune(now)
	tps := make([]float64, len(a.tokens))
	for i, s := range a.tokens {
		tps[i] = s.val
	}
	rps := make([]float64, len(a.requests))
	for i, s := range a.requests {
		rps[i] = s.val
	}
	return Metrics{TokensPerSec: tps, RequestsPerSec: rps, WindowSeconds: int(a.window.Seconds())}
}
