//go:build !windows && !linux

package main

import "errors"

func enableTerminalOutput() func() { return func() {} }

func withRawTerminal(_ func() error) error {
	return errors.New("arrow-key navigation is not available on this terminal")
}
