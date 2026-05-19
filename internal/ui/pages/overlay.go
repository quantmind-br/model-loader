package pages

// Overlay describes an active modal overlay that should be rendered on top
// of the page content.
type Overlay struct {
	Content string
	Width   int
	Height  int
	Active  bool
}
