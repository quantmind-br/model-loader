package httpproxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

// fixJSON best-effort repairs tool-call arguments some local models emit with
// single quotes (Python-style dicts). It is conservative: it only rewrites when
// the input is NOT already valid JSON and the single→double-quote conversion
// yields valid JSON — otherwise the input is returned unchanged, so valid JSON
// (including apostrophes inside double-quoted strings) is never corrupted.
func fixJSON(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}
	if converted := convertSingleQuotes(s); json.Valid([]byte(converted)) {
		return converted
	}
	return s
}

// convertSingleQuotes rewrites single-quoted JSON strings as double-quoted ones,
// tracking string state so quotes inside the other kind of string are handled.
func convertSingleQuotes(input string) string {
	var out strings.Builder
	inDouble, inSingle, escaped := false, false, false
	for _, r := range input {
		switch {
		case inDouble:
			out.WriteRune(r)
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inDouble = false
			}
		case inSingle:
			switch {
			case escaped:
				escaped = false
				switch r {
				case '\'':
					out.WriteRune('\'') // \' → literal '
				case '\\':
					out.WriteString(`\\`)
				default:
					out.WriteByte('\\')
					out.WriteRune(r)
				}
			case r == '\\':
				escaped = true
			case r == '\'':
				inSingle = false
				out.WriteByte('"') // close single-quoted string as double
			case r == '"':
				out.WriteString(`\"`) // escape a literal " inside the converted string
			default:
				out.WriteRune(r)
			}
		default:
			switch r {
			case '"':
				inDouble = true
				out.WriteRune(r)
			case '\'':
				inSingle = true
				out.WriteByte('"') // open single-quoted string as double
			default:
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

var toolIDInvalidChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeToolID forces a tool id to Anthropic's `^[A-Za-z0-9_-]+$` shape,
// replacing runs of invalid characters with "_" and generating a random
// toolu_ id when the input is empty or wholly invalid.
func sanitizeToolID(id string) string {
	s := toolIDInvalidChars.ReplaceAllString(id, "_")
	if s == "" || s == "_" {
		var b [8]byte
		_, _ = rand.Read(b[:])
		return "toolu_" + hex.EncodeToString(b[:])
	}
	return s
}
