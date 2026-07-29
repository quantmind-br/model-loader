package pages

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func TestBackendsPage_DenseRows(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	for i := range 12 {
		addBackendForPage(t, mgr, fmt.Sprintf("Backend %d", i), "/bin/echo")
	}
	p = loadBackendsPage(t, p)
	p.list.SetSize(80, 12)

	if got, want := p.list.Paginator.PerPage, 10; got != want {
		t.Fatalf("PerPage = %d, want %d", got, want)
	}
}

func TestProfilesPage_DenseRows(t *testing.T) {
	store, err := profilestore.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{})
	page.list.SetSize(80, 12)
	profiles := make([]domain.Profile, 12)
	for i := range profiles {
		profiles[i] = domain.Profile{ID: fmt.Sprintf("profile-%d", i), Name: fmt.Sprintf("Profile %d", i), Model: "/m.gguf"}
	}
	updated, _ := page.Update(loadedMsg{profiles: profiles})
	page = updated.(ProfilesPage)

	if got, want := page.list.Paginator.PerPage, 10; got != want {
		t.Fatalf("PerPage = %d, want %d", got, want)
	}
}

func TestProfilesPage_RendersPaginatorFooter(t *testing.T) {
	store, err := profilestore.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{})
	page.width, page.height = 200, 40
	page.list.SetSize(80, 12)
	profiles := make([]domain.Profile, 25)
	for i := range profiles {
		profiles[i] = domain.Profile{ID: fmt.Sprintf("profile-%02d", i), Name: fmt.Sprintf("Profile %d", i), Model: "/m.gguf"}
	}
	updated, _ := page.Update(loadedMsg{profiles: profiles})
	page = updated.(ProfilesPage)

	if out := page.View(); !strings.Contains(out, "page 1/3 · 25 profiles") {
		t.Fatalf("first footer missing; got:\n%s", out)
	}
	page.list.NextPage()
	if out := page.View(); !strings.Contains(out, "page 2/3 · 25 profiles") {
		t.Fatalf("second footer missing; got:\n%s", out)
	}
	page.list.PrevPage()
	if out := page.View(); !strings.Contains(out, "page 1/3 · 25 profiles") {
		t.Fatalf("first footer missing after previous page; got:\n%s", out)
	}
}

func TestBackendsPage_RendersPaginatorFooter(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	for i := range 25 {
		addBackendForPage(t, mgr, fmt.Sprintf("Backend %02d", i), "/bin/echo")
	}
	p = loadBackendsPage(t, p)
	p.list.SetSize(80, 12)

	if out := p.View(); !strings.Contains(out, "page 1/3 · 25 backends") {
		t.Fatalf("first footer missing; got:\n%s", out)
	}
	p.list.NextPage()
	if out := p.View(); !strings.Contains(out, "page 2/3 · 25 backends") {
		t.Fatalf("second footer missing; got:\n%s", out)
	}
	p.list.PrevPage()
	if out := p.View(); !strings.Contains(out, "page 1/3 · 25 backends") {
		t.Fatalf("first footer missing after previous page; got:\n%s", out)
	}
}

func TestProfilesPage_EmptyListHasNoPaginatorFooter(t *testing.T) {
	store, err := profilestore.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{})
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	page = updated.(ProfilesPage)

	if out := page.View(); strings.Contains(out, "page ") {
		t.Fatalf("empty list rendered paginator footer:\n%s", out)
	}
}

func TestBackendsPage_ListOmitsBackendID(t *testing.T) {
	p, _, _ := newBackendsPageHarness(t)
	backend := domain.Backend{
		ID:          "llama.cpp-stable",
		Name:        "Llama Stable",
		Kind:        domain.BackendKindLlamaServer,
		SchemaRef:   "schemas/default.json",
		Executable:  "/usr/bin/llama-server",
		Description: "Local server",
		Tags:        []string{"cuda"},
	}
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	p = updated.(BackendsPage)
	updated, _ = p.Update(backendsLoadedMsg{backends: []domain.Backend{backend}})
	p = updated.(BackendsPage)

	if got := strings.Count(p.View(), backend.ID); got != 1 {
		t.Fatalf("backend ID appears %d times, want exactly 1:\n%s", got, p.View())
	}
}
