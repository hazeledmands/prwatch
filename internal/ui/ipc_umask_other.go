//go:build !unix

package ui

import "net"

// listenUnixPrivate binds the socket. Platforms without a umask (Windows) get
// the plain bind; the chmod in StartIPCListener still runs, and the parent
// directory's permissions remain the real protection.
func listenUnixPrivate(socketPath string) (net.Listener, error) {
	return net.Listen("unix", socketPath)
}
