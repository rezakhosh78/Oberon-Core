package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

var errElevationRelaunched = errors.New("restarted with Administrator privileges")

func dispatchCLI(args []string) (bool, error) {
	if len(args) == 0 {
		return true, runTUI()
	}
	switch args[0] {
	case "tui":
		if len(args) != 1 {
			return true, fmt.Errorf("usage: oberon tui")
		}
		return true, runTUI()
	case "help", "--help", "-h":
		printCLIUsage()
		return true, nil
	case "--version", "-version":
		fmt.Printf("oberon %s\n\nUserspace AmneziaWG daemon for %s-%s.\nInformation available at https://amnezia.org\n", Version, runtime.GOOS, runtime.GOARCH)
		return true, nil
	case "create":
		return true, runCreateCLI(args[1:])
	case "warp":
		return true, runWarpCLI(args[1:])
	case "scan":
		return true, runScanCLI(args[1:])
	case "connect", "run":
		return true, runConnectCLI(args[1:])
	default:
		return false, nil
	}
}

func printCLIUsage() {
	fmt.Printf(`Oberon %s

Commands:
  create  Register WARP, scan endpoints, and save an AWG profile
  scan    Scan endpoints in an existing AWG profile
  connect Scan on every run, then start the tunnel or local proxies
  tui     Open the interactive terminal menu

Examples:
  oberon create -out warp-awg.conf
  oberon scan -config warp-awg.conf -mode fast
  oberon connect -config warp-awg.conf
  oberon connect -config warp-awg.conf -socks 127.0.0.1:1717 -http 127.0.0.1:1718

Use "oberon <command> -h" for command options.
`, Version)
}

func newScanProgressReporter(total int) func(scanProgress) {
	if total <= 0 {
		return nil
	}
	terminal := false
	if info, err := os.Stderr.Stat(); err == nil {
		terminal = info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb"
	}
	color := terminal && os.Getenv("NO_COLOR") == ""
	paint := func(code, value string) string {
		if !color {
			return value
		}
		return "\x1b[" + code + "m" + value + "\x1b[0m"
	}
	step := total / 90
	if step < 1 {
		step = 1
	}
	last := 0
	return func(state scanProgress) {
		if state.done < total && state.done-last < step {
			return
		}
		last = state.done
		if terminal {
			const width = 30
			filled := state.done * width / state.total
			bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
			fmt.Fprintf(os.Stderr, "\r\033[2K%s %s %3d%%  %d/%d  %s handshakes", paint("36", "Scan"), paint("32", bar), state.done*100/state.total, state.done, state.total, paint("32", fmt.Sprint(state.found)))
			if state.done == state.total {
				fmt.Fprintln(os.Stderr)
			}
			return
		}
		if state.done%10 == 0 || state.done == state.total {
			fmt.Fprintf(os.Stderr, "Scanned %d/%d endpoints; %d handshakes found\n", state.done, state.total, state.found)
		}
	}
}
