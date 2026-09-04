//go:build linux

package sdnotify

import (
	"fmt"
	"net"
)

// send delivers state to the sd_notify socket. A leading @ denotes an
// abstract socket, which Go's net package supports natively for unixgram.
func send(sock, state string) error {
	conn, err := net.Dial("unixgram", sock)
	if err != nil {
		return fmt.Errorf("sd_notify dial %q: %w", sock, err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(state)); err != nil {
		return fmt.Errorf("sd_notify write: %w", err)
	}
	return nil
}
