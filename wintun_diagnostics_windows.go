//go:build windows

package main

import (
	"crypto/sha256"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func loadedWintunDLLInfo() string {
	name, err := windows.UTF16PtrFromString("wintun.dll")
	if err != nil {
		return ""
	}
	var module windows.Handle
	const getModuleHandleExFlagUnchangedRefcount = 0x00000002
	if err := windows.GetModuleHandleEx(getModuleHandleExFlagUnchangedRefcount, name, &module); err != nil {
		return ""
	}
	path16 := make([]uint16, 32768)
	n, err := windows.GetModuleFileName(module, &path16[0], uint32(len(path16)))
	if err != nil || n == 0 || int(n) >= len(path16) {
		return ""
	}
	path := windows.UTF16ToString(path16[:n])
	contents, err := os.ReadFile(path)
	if err != nil {
		return path
	}
	sum := sha256.Sum256(contents)
	return fmt.Sprintf("%s (SHA-256 %x)", path, sum)
}
