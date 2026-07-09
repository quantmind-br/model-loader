package httpproxy

import "io"

// prefixedBody replays buffered bytes then continues with the unread
// remainder of the original body. Close closes the original.
type prefixedBody struct {
	io.Reader
	closer io.ReadCloser
}

func (b *prefixedBody) Close() error { return b.closer.Close() }
