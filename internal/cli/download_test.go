package cli

import "testing"

func TestDownloadCommand_HiddenAndPositional(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"download"})
	if err != nil || cmd == nil || cmd.Name() != "download" {
		t.Fatalf("download command not registered: cmd=%v err=%v", cmd, err)
	}
	if !cmd.Hidden {
		t.Fatalf("download worker command must be hidden from help")
	}
}
