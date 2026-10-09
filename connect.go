package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func runConnectCLI(args []string) error {
	flags := flag.NewFlagSet("connect", flag.ContinueOnError)
	config := flags.String("config", "", "AWG configuration file")
	configAlias := flags.String("conf", "", "AWG configuration file (alias for -config)")
	mode := flags.String("mode", "fast", "endpoint scan pool: fast or all")
	workers := flags.Int("workers", 16, "parallel endpoint checks")
	timeout := flags.Duration("timeout", 900*time.Millisecond, "handshake timeout per endpoint")
	socks := flags.String("socks", "", "start a local SOCKS5 proxy, for example 127.0.0.1:1717")
	httpProxy := flags.String("http", "", "start a local HTTP proxy, for example 127.0.0.1:1718")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	configPath := *config
	if configPath == "" {
		configPath = *configAlias
	}
	if configPath == "" && flags.NArg() > 0 {
		configPath = flags.Arg(0)
	}
	if configPath == "" {
		return fmt.Errorf("usage: oberon connect -config profile.conf [-mode fast|all] [-socks 127.0.0.1:1717] [-http 127.0.0.1:1718]")
	}
	cfg, err := parseAWGConfig(configPath)
	if err != nil {
		return err
	}
	candidates, err := endpointCandidates(strings.ToLower(*mode))
	if err != nil {
		return err
	}
	proxyMode := *socks != "" || *httpProxy != ""
	if proxyMode {
		if *socks != "" {
			if err := validateLocalProxyAddr(*socks); err != nil {
				return fmt.Errorf("SOCKS5 listen address: %w", err)
			}
		}
		if *httpProxy != "" {
			if err := validateLocalProxyAddr(*httpProxy); err != nil {
				return fmt.Errorf("HTTP listen address: %w", err)
			}
		}
	} else {
		launched, err := ensureAdministrator(append([]string{"connect"}, args...))
		if err != nil {
			return err
		}
		if launched {
			return errElevationRelaunched
		}
	}

	fmt.Fprintf(os.Stderr, "Scanning endpoints before connecting (%d candidates)...\n", len(candidates))
	hits, err := scanEndpointsWithProgress(cfg, candidates, *workers, *timeout, newScanProgressReporter(len(candidates)))
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		return fmt.Errorf("no endpoint completed an AmneziaWG handshake; the tunnel was not started")
	}
	cfg.peer["endpoint"] = hits[0].Endpoint
	fmt.Fprintf(os.Stderr, "Selected endpoint: %s (%d ms)\n", hits[0].Endpoint, hits[0].Latency.Milliseconds())
	if proxyMode {
		return runProxySession(cfg, *socks, *httpProxy)
	}
	return runConfigProfile(cfg)
}
