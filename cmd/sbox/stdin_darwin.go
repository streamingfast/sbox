//go:build darwin

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
	err := syscall.Select(fd+1, fdSet, nil, nil, timeout)
	if err != nil {
		return false
	}
	// On Darwin we check whether our fd bit is still set in the returned fdSet,
	// since the return value carries no count (unlike Linux).
	return fdIsSet(fdSet, fd)
}

// fdSetAdd sets a file descriptor in an FdSet.
// On Darwin, FdSet.Bits elements are int32.
func fdSetAdd(set *syscall.FdSet, fd int) {
	set.Bits[fd/32] |= 1 << (uint(fd) % 32)
}

// fdIsSet reports whether fd is set in the FdSet.
func fdIsSet(set *syscall.FdSet, fd int) bool {
	return set.Bits[fd/32]&(1<<(uint(fd)%32)) != 0
}
