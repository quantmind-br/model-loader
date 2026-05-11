package processmgr

import (
	"fmt"
	"strings"
	"unicode"
)

// splitCommandLine performs basic shell-like tokenization on a command string.
// It supports:
//   - unquoted tokens separated by whitespace
//   - double-quoted tokens ("...") with escaped quotes (\")
//   - single-quoted tokens ('...')
//
// It returns an error for unbalanced quotes.
func splitCommandLine(raw string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inDouble := false
	inSingle := false
	escaped := false

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for _, r := range raw {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			if inSingle {
				current.WriteRune(r)
			} else {
				escaped = true
			}
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if unicode.IsSpace(r) && !inDouble && !inSingle {
			flush()
			continue
		}
		current.WriteRune(r)
	}

	if inDouble || inSingle {
		return nil, fmt.Errorf("unbalanced quote in command: %q", raw)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash in command: %q", raw)
	}
	flush()
	return tokens, nil
}

// exeFromBinaryPath extracts the executable token from a binary path or
// compound command string.
func exeFromBinaryPath(binary string) string {
	tokens, err := splitCommandLine(binary)
	if err != nil || len(tokens) == 0 {
		return binary
	}
	return tokens[0]
}
