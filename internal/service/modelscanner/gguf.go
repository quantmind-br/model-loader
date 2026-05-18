package modelscanner

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ggufMagic is the 4-byte file signature.
var ggufMagic = [4]byte{'G', 'G', 'U', 'F'}

// errBadMagic is returned when the first 4 bytes do not match "GGUF".
var errBadMagic = errors.New("modelscanner: not a GGUF file (bad magic)")

// ggufHeader holds the fixed-width prefix every GGUF file starts with.
type ggufHeader struct {
	Version       uint32
	TensorCount   uint64
	MetadataCount uint64
}

// readGGUFHeader reads and validates the GGUF magic + version + counts.
// It does NOT advance into metadata KV pairs.
func readGGUFHeader(r io.Reader) (ggufHeader, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return ggufHeader{}, fmt.Errorf("read magic: %w", err)
	}
	if magic != ggufMagic {
		return ggufHeader{}, errBadMagic
	}
	var hdr ggufHeader
	if err := binary.Read(r, binary.LittleEndian, &hdr.Version); err != nil {
		return ggufHeader{}, fmt.Errorf("read version: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &hdr.TensorCount); err != nil {
		return ggufHeader{}, fmt.Errorf("read tensor count: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &hdr.MetadataCount); err != nil {
		return ggufHeader{}, fmt.Errorf("read metadata count: %w", err)
	}
	return hdr, nil
}

// GGUF metadata value type IDs (subset we support).
// Source: github.com/ggerganov/ggml — ggml/include/gguf.h.
const (
	ggufTypeUint8   uint32 = 0
	ggufTypeInt8    uint32 = 1
	ggufTypeUint16  uint32 = 2
	ggufTypeInt16   uint32 = 3
	ggufTypeUint32  uint32 = 4
	ggufTypeInt32   uint32 = 5
	ggufTypeFloat32 uint32 = 6
	ggufTypeBool    uint32 = 7
	ggufTypeString  uint32 = 8
	ggufTypeUint64  uint32 = 10
	ggufTypeInt64   uint32 = 11
	ggufTypeFloat64 uint32 = 12
)

// metadataScanLimit caps how many KV pairs we walk before giving up.
// Real GGUFs have hundreds of KVs; we only need general.parameter_count
// or general.size_label which are usually within the first ~50.
const metadataScanLimit = 128

type ggufMeta struct {
	Params       string
	Architecture string
	BlockCount   uint64
}

func readGGUFMeta(r io.Reader) (ggufMeta, error) {
	hdr, err := readGGUFHeader(r)
	if err != nil {
		return ggufMeta{}, err
	}
	scan := hdr.MetadataCount
	if scan > metadataScanLimit {
		scan = metadataScanLimit
	}

	var params string
	var arch string
	blockCounts := make(map[string]uint64)

	for i := uint64(0); i < scan; i++ {
		key, err := readGGUFString(r)
		if err != nil {
			return ggufMeta{}, nil
		}
		var typeID uint32
		if err := binary.Read(r, binary.LittleEndian, &typeID); err != nil {
			return ggufMeta{}, nil
		}

		switch key {
		case "general.parameter_count":
			if typeID != ggufTypeUint64 {
				return ggufMeta{}, nil
			}
			var n uint64
			if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
				return ggufMeta{}, nil
			}
			params = formatParams(n)
		case "general.size_label":
			if typeID != ggufTypeString {
				return ggufMeta{}, nil
			}
			s, err := readGGUFString(r)
			if err != nil {
				return ggufMeta{}, nil
			}
			params = s
		case "general.architecture":
			if typeID != ggufTypeString {
				return ggufMeta{}, nil
			}
			s, err := readGGUFString(r)
			if err != nil {
				return ggufMeta{}, nil
			}
			arch = s
		default:
			if strings.HasSuffix(key, ".block_count") && typeID == ggufTypeUint64 {
				var n uint64
				if err := binary.Read(r, binary.LittleEndian, &n); err == nil {
					prefix := strings.TrimSuffix(key, ".block_count")
					blockCounts[prefix] = n
				}
			} else {
				if !skipGGUFValue(r, typeID) {
					return ggufMeta{}, nil
				}
			}
		}
	}

	var blockCount uint64
	if arch != "" {
		if bc, ok := blockCounts[arch]; ok {
			blockCount = bc
		}
	}

	return ggufMeta{Params: params, Architecture: arch, BlockCount: blockCount}, nil
}

// readGGUFParams reads the full header and walks metadata KV pairs
// looking for parameter-count signals. Returns "" when neither key is
// present, when an unsupported value type is encountered (we cannot
// safely advance the reader past unknown payloads), or when the file
// is truncated. Caller must seek/wrap the reader to the start.
func readGGUFParams(r io.Reader) (string, error) {
	m, err := readGGUFMeta(r)
	if err != nil {
		return "", err
	}
	return m.Params, nil
}

// readGGUFString reads a GGUF string (u64 length + utf8 bytes).
// Length is capped at 1 MiB to defend against bogus headers.
func readGGUFString(r io.Reader) (string, error) {
	var n uint64
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	if n > 1<<20 {
		return "", fmt.Errorf("string length %d exceeds limit", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// skipGGUFValue advances the reader past a metadata value of the given
// type. Returns false for types we cannot skip safely (arrays, unknown).
func skipGGUFValue(r io.Reader, typeID uint32) bool {
	switch typeID {
	case ggufTypeUint8, ggufTypeInt8, ggufTypeBool:
		return discard(r, 1)
	case ggufTypeUint16, ggufTypeInt16:
		return discard(r, 2)
	case ggufTypeUint32, ggufTypeInt32, ggufTypeFloat32:
		return discard(r, 4)
	case ggufTypeUint64, ggufTypeInt64, ggufTypeFloat64:
		return discard(r, 8)
	case ggufTypeString:
		_, err := readGGUFString(r)
		return err == nil
	default:
		// Arrays (9) and unknowns: abort.
		return false
	}
}

func discard(r io.Reader, n int64) bool {
	_, err := io.CopyN(io.Discard, r, n)
	return err == nil
}

// formatParams turns a parameter count like 32_000_000_000 into "32B".
// Below 1B uses M; below 1M uses raw integer; above 1B uses B with one
// decimal when not a round multiple.
func formatParams(n uint64) string {
	switch {
	case n >= 1_000_000_000:
		whole := n / 1_000_000_000
		rem := n % 1_000_000_000
		if rem < 50_000_000 { // round
			return fmt.Sprintf("%dB", whole)
		}
		tenths := (rem + 50_000_000) / 100_000_000
		if tenths == 10 {
			return fmt.Sprintf("%dB", whole+1)
		}
		return fmt.Sprintf("%d.%dB", whole, tenths)
	case n >= 1_000_000:
		return fmt.Sprintf("%dM", n/1_000_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
