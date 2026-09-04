//go:build !linux

package sdnotify

// send is a no-op outside Linux: sd_notify sockets only exist under a
// systemd supervisor, so a non-Linux binary never has one to write to.
func send(sock, state string) error {
	return nil
}
