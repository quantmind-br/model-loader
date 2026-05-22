package pages

import (
	"testing"
)

func TestWebEditMsgs_OpenAndResult(t *testing.T) {
	started := webEditStartedMsg{url: "http://127.0.0.1:1234"}
	if started.url == "" {
		t.Fatal("url should be set")
	}
	done := webEditDoneMsg{saved: true, profileID: "qwen"}
	if !done.saved || done.profileID != "qwen" {
		t.Fatalf("done msg wrong: %+v", done)
	}
}
