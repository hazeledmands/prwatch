//go:build unix

package ui

import (
	"net"
	"syscall"
)

// listenUnixPrivate binds the socket with a umask that denies group and other,
// closing the window between bind and chmod during which the socket sits at
// 0777&^umask and is connectable.
//
// umask is process-global, so this briefly affects any file another goroutine
// creates. StartIPCListener runs once during startup, before the TUI or the
// watcher are going, so there is nothing else creating files at that moment.
func listenUnixPrivate(socketPath string) (net.Listener, error) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	return net.Listen("unix", socketPath)
}
