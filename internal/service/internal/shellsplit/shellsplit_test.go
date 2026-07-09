package shellsplit

import (
	"reflect"
	"testing"
)

func TestSplit_Simple(t *testing.T) {
	got, err := Split("python -m sglang.launch_server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"python", "-m", "sglang.launch_server"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplit_DoubleQuotes(t *testing.T) {
	got, err := Split(`"/path with spaces/python" -m sglang.launch_server`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"/path with spaces/python", "-m", "sglang.launch_server"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplit_SingleQuotes(t *testing.T) {
	got, err := Split(`python -m 'sglang.launch_server'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"python", "-m", "sglang.launch_server"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplit_EscapedSpace(t *testing.T) {
	got, err := Split("python -m sglang\\ launch_server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"python", "-m", "sglang launch_server"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplit_UnbalancedQuote(t *testing.T) {
	_, err := Split(`"unbalanced`)
	if err == nil {
		t.Fatal("expected error for unbalanced quote")
	}
}

func TestSplit_EscapedQuoteInsideDouble(t *testing.T) {
	got, err := Split(`python -c "print(\"hi\")"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"python", "-c", `print("hi")`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSplit_LiteralBackslashInSingleQuote(t *testing.T) {
	got, err := Split(`python -c 'print(\\hi)'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"python", "-c", `print(\\hi)`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
