package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// childByName returns the immediate subcommand of parent whose first Use word
// matches name, or nil.
func childByName(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestModelCmd_RegisteredUnderRoot(t *testing.T) {
	if childByName(rootCmd, "model") == nil {
		t.Fatal("model command not registered under root")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{-1, "?"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
