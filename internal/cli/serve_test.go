package cli

import "testing"

func TestServeCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"serve"})
	if err != nil || cmd == nil || cmd.Name() != "serve" {
		t.Fatalf("serve command not registered: cmd=%v err=%v", cmd, err)
	}
	if cmd.Flags().Lookup("host") == nil || cmd.Flags().Lookup("port") == nil {
		t.Fatalf("serve must expose --host and --port flags")
	}
}
