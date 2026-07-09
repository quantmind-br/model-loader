package processmgr

import "github.com/quantmind-br/model-loader/internal/service/internal/shellsplit"

// exeFromBinaryPath extracts the executable token from a binary path or
// compound command string.
func exeFromBinaryPath(binary string) string {
	tokens, err := shellsplit.Split(binary)
	if err != nil || len(tokens) == 0 {
		return binary
	}
	return tokens[0]
}
