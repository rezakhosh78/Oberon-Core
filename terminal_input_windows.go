//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableTerminalOutput() func() {
	const enableVirtualTerminalProcessing = 0x0004
	output := windows.Handle(os.Stdout.Fd())
	var previous uint32
	if err := windows.GetConsoleMode(output, &previous); err != nil {
		return func() {}
	}
	if err := windows.SetConsoleMode(output, previous|enableVirtualTerminalProcessing); err != nil {
		return func() {}
	}
	return func() { _ = windows.SetConsoleMode(output, previous) }
}

func withRawTerminal(read func() error) error {
	const (
		enableProcessedInput      = 0x0001
		enableLineInput           = 0x0002
		enableEchoInput           = 0x0004
		enableVirtualTerminalInput = 0x0200
	)
	input := windows.Handle(os.Stdin.Fd())
	var previous uint32
	if err := windows.GetConsoleMode(input, &previous); err != nil {
		return err
	}
	current := previous &^ (enableLineInput | enableEchoInput)
	current |= enableProcessedInput | enableVirtualTerminalInput
	if err := windows.SetConsoleMode(input, current); err != nil {
		return err
	}
	defer func() { _ = windows.SetConsoleMode(input, previous) }()
	return read()
}
