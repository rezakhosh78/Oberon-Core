//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func enableTerminalOutput() func() { return func() {} }

func withRawTerminal(read func() error) error {
	fd := int(os.Stdin.Fd())
	previous, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	current := *previous
	current.Lflag &^= unix.ICANON | unix.ECHO
	current.Cc[unix.VMIN] = 1
	current.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &current); err != nil {
		return err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, previous)
	return read()
}
