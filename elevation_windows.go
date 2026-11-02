//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var shell32 = syscall.NewLazyDLL("shell32.dll")

func ensureAdministrator(args []string) (bool, error) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return false, nil
	}

	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("find Oberon executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return false, fmt.Errorf("resolve Oberon executable path: %w", err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return false, fmt.Errorf("read current directory: %w", err)
	}
	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	filePtr, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return false, err
	}
	parameters := make([]string, len(args))
	for i, arg := range args {
		parameters[i] = escapeWindowsArgument(arg)
	}
	parametersPtr, err := syscall.UTF16PtrFromString(strings.Join(parameters, " "))
	if err != nil {
		return false, err
	}
	directoryPtr, err := syscall.UTF16PtrFromString(workingDirectory)
	if err != nil {
		return false, err
	}
	proc := shell32.NewProc("ShellExecuteW")
	if err := proc.Find(); err != nil {
		return false, fmt.Errorf("request Windows elevation: %w", err)
	}
	result, _, callErr := proc.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(parametersPtr)),
		uintptr(unsafe.Pointer(directoryPtr)),
		1,
	)
	runtime.KeepAlive(verbPtr)
	runtime.KeepAlive(filePtr)
	runtime.KeepAlive(parametersPtr)
	runtime.KeepAlive(directoryPtr)
	if result <= 32 {
		if callErr == syscall.Errno(1223) {
			return false, fmt.Errorf("Administrator approval was cancelled")
		}
		return false, fmt.Errorf("could not restart Oberon as Administrator (ShellExecuteW result %d): %v", result, callErr)
	}
	return true, nil
}

func escapeWindowsArgument(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\v\"") {
		return value
	}
	var out strings.Builder
	out.WriteByte('"')
	backslashes := 0
	for _, char := range value {
		if char == '\\' {
			backslashes++
			continue
		}
		if char == '"' {
			out.WriteString(strings.Repeat("\\", backslashes*2+1))
			out.WriteRune(char)
			backslashes = 0
			continue
		}
		out.WriteString(strings.Repeat("\\", backslashes))
		backslashes = 0
		out.WriteRune(char)
	}
	out.WriteString(strings.Repeat("\\", backslashes*2))
	out.WriteByte('"')
	return out.String()
}
