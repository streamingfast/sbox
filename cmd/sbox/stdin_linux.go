//go:build linux

package main

import (
	"os"
	"syscall"
)

func stdinHasData() bool {
	fd := int(os.Stdin.Fd())
	fdSet := &syscall.FdSet{}
	fdSetAdd(fdSet, fd)
	timeout := &syscall.Timeval{Sec: 0, Usec: 0}
	n, err := syscall.Select(fd+1, fdSet, nil, nil, timeout)
	return err == nil && n > 0
}

// fdSetAdd sets a file descriptor in an FdSet.
// On Linux, FdSet.Bits elements are int64.
func fdSetAdd(set *syscall.FdSet, fd int) {
	set.Bits[fd/64] |= 1 << (uint(fd) % 64)
}
