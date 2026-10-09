package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func runWarpCLI(args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("usage: oberon warp create [-endpoint host:port] [-out file] [-mode fast|all]")
	}
	return runCreateCLI(args[1:])
}

func runCreateCLI(args []string) error {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	endpoint := flags.String("endpoint", "", "also scan this endpoint")
	output := flags.String("out", "warp-awg.conf", "write the generated profile to this file")
	mode := flags.String("mode", "fast", "candidate set: fast or all")
	workers := flags.Int("workers", 16, "parallel endpoint checks")
	probeTimeout := flags.Duration("timeout", 900*time.Millisecond, "handshake timeout per endpoint")
	registrationTimeout := flags.Duration("registration-timeout", 20*time.Second, "WARP registration request timeout")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	candidates, err := endpointCandidates(strings.ToLower(*mode))
	if err != nil {
		return err
	}
	if *endpoint != "" {
		candidates = append([]string{*endpoint}, candidates...)
	}
	client := newWarpHTTPClient(*registrationTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), *registrationTimeout)
	defer cancel()
	fmt.Fprintf(os.Stderr, "Registering WARP account with Oberon %s...\n", Version)
	profile, err := registerWarp(ctx, client)
	if err != nil {
		return err
	}
	cfg := warpProfileConfig(profile)
	hits, err := scanEndpointsWithProgress(cfg, candidates, *workers, *probeTimeout, newScanProgressReporter(len(candidates)))
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		return fmt.Errorf("no endpoint completed an AmneziaWG handshake")
	}
	profile.Endpoint = hits[0].Endpoint
	config := renderWarpConfig(profile, profile.Endpoint)
	if err := os.WriteFile(*output, []byte(config), 0600); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\nBest endpoint: %s · %d ms\nWARP AWG profile saved to %s\n", profile.Endpoint, hits[0].Latency.Milliseconds(), *output)
	return nil
}

func warpProfileConfig(profile warpProfile) awgConfig {
	return awgConfig{iface: map[string]string{
		"privatekey": profile.PrivateKey,
		"address": profile.Address,
		"dns": "1.1.1.1, 1.0.0.1, 2606:4700:4700::1111, 2606:4700:4700::1001",
		"mtu": fmt.Sprint(profile.MTU), "jc": "5", "jmin": "10", "jmax": "40", "s1": "0", "s2": "0", "h1": "1", "h2": "2", "h3": "3", "h4": "4",
	}, peer: map[string]string{"publickey": profile.PeerPublicKey, "endpoint": profile.Endpoint, "allowedips": "0.0.0.0/0, ::/0"}}
}
