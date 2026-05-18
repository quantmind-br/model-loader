package domain

// ModelFile describes a discovered GGUF file on disk.
// Quant and Params are best-effort: derived from filename heuristics
// (and GGUF metadata when readable). Either may be empty.
type ModelFile struct {
	Path         string // absolute path
	SizeBytes    int64
	Name         string // base filename
	Quant        string // e.g. "Q4_K_M", "Q5_K_M", "Q8_0"
	Params       string // e.g. "32B", "7B"
	Architecture string
	BlockCount   uint64
}

// ScanEventType discriminates ScanEvent payloads.
type ScanEventType int

const (
	// ScanEventFile is emitted once per discovered .gguf file.
	ScanEventFile ScanEventType = iota
	// ScanEventProgress is emitted at the start of each root path scan
	// (Count == 0) and again at end (Count == final file count).
	ScanEventProgress
	// ScanEventError is emitted when a root path fails (e.g., ENOENT).
	// Per-file read errors do not abort the scan; they are silently
	// degraded to a ModelFile with empty Quant/Params.
	// Terminal for that root: no ScanEventProgress follows on error.
	ScanEventError
	// ScanEventDone is emitted exactly once after all roots are visited.
	ScanEventDone
)

// ScanEvent is the channel payload from ModelScanner.Scan.
// Root is the configured search path the event is attributed to (empty
// for ScanEventDone, which is global).
type ScanEvent struct {
	Type  ScanEventType
	Root  string
	File  *ModelFile
	Count int
	Error error
}
