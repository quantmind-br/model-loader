package cli

import "testing"

func TestInstanceSubcommandsRegistered(t *testing.T) {
	for _, name := range []string{"list", "show", "history", "start", "stop", "restart", "logs", "metrics"} {
		cmd, _, err := rootCmd.Find([]string{"instance", name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("instance %s not registered: cmd=%v err=%v", name, cmd, err)
		}
	}
}
