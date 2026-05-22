package cli

import "testing"

func TestProfileSubcommandsRegistered(t *testing.T) {
	for _, name := range []string{"list", "show", "create", "edit", "delete", "duplicate", "rename", "pin", "unpin", "export", "import", "validate"} {
		cmd, _, err := rootCmd.Find([]string{"profile", name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("profile %s not registered: cmd=%v err=%v", name, cmd, err)
		}
	}
}
