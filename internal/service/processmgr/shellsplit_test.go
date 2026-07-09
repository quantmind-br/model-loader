package processmgr

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestExeFromBinaryPath_Simple(t *testing.T) {
	if got := exeFromBinaryPath("python -m sglang.launch_server"); got != "python" {
		t.Errorf("got %q, want python", got)
	}
}

func TestExeFromBinaryPath_Quoted(t *testing.T) {
	if got := exeFromBinaryPath(`"/path with spaces/python" -m sglang.launch_server`); got != "/path with spaces/python" {
		t.Errorf("got %q, want /path with spaces/python", got)
	}
}

func TestExeFromBinaryPath_SinglePath(t *testing.T) {
	if got := exeFromBinaryPath("/usr/bin/python"); got != "/usr/bin/python" {
		t.Errorf("got %q, want /usr/bin/python", got)
	}
}

func TestMakeCommand_Simple(t *testing.T) {
	cmd := makeCommand("python", []string{"--model-path", "/m"})
	want := []string{"python", "--model-path", "/m"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("Args = %v, want %v", cmd.Args, want)
	}
}

func TestMakeCommand_Compound(t *testing.T) {
	cmd := makeCommand("python -m sglang.launch_server", []string{"--model-path", "/m"})
	want := []string{"python", "-m", "sglang.launch_server", "--model-path", "/m"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("Args = %v, want %v", cmd.Args, want)
	}
}

func TestMakeCommand_QuotedPath(t *testing.T) {
	cmd := makeCommand(`"/path with spaces/python" -m sglang.launch_server`, []string{"--model-path", "/m"})
	want := []string{"/path with spaces/python", "-m", "sglang.launch_server", "--model-path", "/m"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("Args = %v, want %v", cmd.Args, want)
	}
}

func TestLooksLikeHFRepo_DottedID(t *testing.T) {
	if !domain.LooksLikeHFRepo("Qwen/Qwen2.5-7B-Instruct") {
		t.Error("Qwen/Qwen2.5-7B-Instruct should look like HF repo")
	}
	if !domain.LooksLikeHFRepo("meta-llama/Llama-3.1-8B-Instruct") {
		t.Error("meta-llama/Llama-3.1-8B-Instruct should look like HF repo")
	}
	if domain.LooksLikeHFRepo("models/model.gguf") {
		t.Error("models/model.gguf should NOT look like HF repo")
	}
}
