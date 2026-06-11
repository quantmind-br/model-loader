package cli

import (
	"strings"
	"testing"
)

func TestServeCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"serve"})
	if err != nil || cmd == nil || cmd.Name() != "serve" {
		t.Fatalf("serve command not registered: cmd=%v err=%v", cmd, err)
	}
	if cmd.Flags().Lookup("host") == nil || cmd.Flags().Lookup("port") == nil {
		t.Fatalf("serve must expose --host and --port flags")
	}
}

func TestCurlHost(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"0.0.0.0", "127.0.0.1"},
		{"::", "127.0.0.1"},
		{"", "127.0.0.1"},
		{"127.0.0.1", "127.0.0.1"},
		{"192.168.1.10", "192.168.1.10"},
		{"::1", "[::1]"},
		{"fe80::42", "[fe80::42]"},
	}
	for _, c := range cases {
		got := curlHost(c.host)
		if got != c.want {
			t.Errorf("curlHost(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

func TestCommandExamples(t *testing.T) {
	cases := []struct {
		path []string
	}{
		{[]string{"instance", "start"}},
		{[]string{"model", "download"}},
		{[]string{"profile", "validate"}},
	}
	for _, c := range cases {
		cmd, _, err := rootCmd.Find(c.path)
		if err != nil || cmd == nil {
			t.Errorf("command %v not found: %v", c.path, err)
			continue
		}
		if cmd.Example == "" {
			t.Errorf("command %v has no Example", c.path)
		}
		if !strings.Contains(cmd.Example, "model-loader ") {
			t.Errorf("command %v Example does not contain 'model-loader ': %q", c.path, cmd.Example)
		}
	}
}

func TestProfileValidate_LongContainsExitCodes(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"profile", "validate"})
	if err != nil || cmd == nil {
		t.Fatalf("profile validate not found: %v", err)
	}
	if !strings.Contains(cmd.Long, "2") || !strings.Contains(cmd.Long, "blocking") {
		t.Fatalf("profile validate Long should document exit code 2 and 'blocking', got: %q", cmd.Long)
	}
}
