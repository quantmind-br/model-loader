package cli

import "testing"

func TestImportCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"import"})
	if err != nil || cmd == nil || cmd.Name() != "import" {
		t.Fatalf("import command not registered: cmd=%v err=%v", cmd, err)
	}
	if cmd.Flags().Lookup("mode") == nil {
		t.Fatalf("import must expose --mode flag")
	}
}
