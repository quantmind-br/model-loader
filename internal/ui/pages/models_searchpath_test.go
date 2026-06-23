package pages

import "testing"

// TestModelsPage_RemoveBrokenPaths covers the B9 in-app removal of broken
// search paths: errored roots are dropped from the list, persisted via the
// injected callback, and cleared from the status map.
func TestModelsPage_RemoveBrokenPaths(t *testing.T) {
	var persisted []string
	p := NewModelsPage(nil, []string{"/good", "/bad"}).
		WithSearchPathPersister(func(paths []string) error {
			persisted = paths
			return nil
		})
	p.statusMap["/good"] = pathStatus{state: "scanned", count: 3}
	p.statusMap["/bad"] = pathStatus{state: "error", err: "no such dir"}

	if !p.hasErrorRoot() {
		t.Fatal("expected hasErrorRoot() == true")
	}
	if got := p.brokenPaths(); len(got) != 1 || got[0] != "/bad" {
		t.Fatalf("brokenPaths() = %v, want [/bad]", got)
	}

	m, _ := p.performRemoveBrokenPaths()
	next := m.(ModelsPage)
	if len(next.paths) != 1 || next.paths[0] != "/good" {
		t.Errorf("paths = %v, want [/good]", next.paths)
	}
	if len(persisted) != 1 || persisted[0] != "/good" {
		t.Errorf("persisted search paths = %v, want [/good]", persisted)
	}
	if _, ok := next.statusMap["/bad"]; ok {
		t.Error("/bad should have been dropped from statusMap")
	}
}

// TestModelsPage_RemoveBrokenPaths_NoPersister verifies the action still
// removes paths for the session when no persister is wired (nil-safe).
func TestModelsPage_RemoveBrokenPaths_NoPersister(t *testing.T) {
	p := NewModelsPage(nil, []string{"/good", "/bad"})
	p.statusMap["/good"] = pathStatus{state: "scanned"}
	p.statusMap["/bad"] = pathStatus{state: "error"}

	m, _ := p.performRemoveBrokenPaths()
	next := m.(ModelsPage)
	if len(next.paths) != 1 || next.paths[0] != "/good" {
		t.Errorf("paths = %v, want [/good] even without a persister", next.paths)
	}
}
