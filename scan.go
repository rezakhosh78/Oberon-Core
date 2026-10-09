package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type endpointHit struct {
	Endpoint string
	Latency  time.Duration
}

func runScanCLI(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	protocol := fs.String("p", "awg", "tunnel protocol (awg)")
	printProfile := fs.Bool("P", false, "print the config with the fastest reachable endpoint")
	configPath := fs.String("conf", "", "AmneziaWG config file")
	configPathLong := fs.String("config", "", "AmneziaWG config file")
	outputPath := fs.String("o", "", "write the selected profile to this file")
	mode := fs.String("mode", "fast", "candidate set: fast or all")
	workers := fs.Int("workers", 16, "parallel endpoint checks")
	timeout := fs.Duration("timeout", 900*time.Millisecond, "handshake timeout per endpoint")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if strings.ToLower(*protocol) != "awg" {
		return fmt.Errorf("only -p awg is supported")
	}
	if *configPath == "" {
		*configPath = *configPathLong
	}
	if *configPath == "" && fs.NArg() > 0 {
		*configPath = fs.Arg(0)
	}
	if *configPath == "" {
		return fmt.Errorf("usage: oberon scan -p awg -P -conf profile.conf [-o output.conf] [-mode fast|all]")
	}
	cfg, err := parseAWGConfig(*configPath)
	if err != nil {
		return err
	}
	candidates, err := endpointCandidates(strings.ToLower(*mode))
	if err != nil {
		return err
	}
	hits, err := scanEndpointsWithProgress(cfg, candidates, *workers, *timeout, newScanProgressReporter(len(candidates)))
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		return fmt.Errorf("no endpoint completed an AmneziaWG handshake")
	}
	if *printProfile {
		content, err := os.ReadFile(*configPath)
		if err != nil {
			return err
		}
		updated := replacePeerEndpoint(string(content), hits[0].Endpoint)
		if *outputPath != "" {
			if err := os.WriteFile(*outputPath, []byte(updated), 0600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Saved profile with %s to %s\n", hits[0].Endpoint, *outputPath)
			return nil
		}
		_, err = fmt.Print(updated)
		return err
	}
	fmt.Println("Endpoint                         Handshake")
	for _, hit := range hits {
		fmt.Printf("%-32s %d ms\n", hit.Endpoint, hit.Latency.Milliseconds())
	}
	fmt.Printf("Selected: %s\n", hits[0].Endpoint)
	return nil
}

func endpointCandidates(mode string) ([]string, error) {
	fast := []string{
		"188.114.97.6:7281", "188.114.97.6:859", "8.6.112.104:4233", "8.6.112.106:3138",
		"8.6.112.107:3138", "8.6.112.121:1180", "8.6.112.122:894", "8.6.112.127:4198",
		"8.6.112.133:968", "8.6.112.139:7281", "8.6.112.154:891", "8.6.112.182:891",
		"188.114.98.27:890", "188.114.99.119:891", "188.114.96.62:894", "188.114.97.124:903",
		"188.114.96.1:1701", "162.159.195.54:864", "162.159.192.60:859", "188.114.97.114:880",
		"162.159.192.121:903", "188.114.97.121:968", "188.114.98.53:890", "162.159.195.56:864",
		"162.159.195.68:864", "162.159.192.132:903", "188.114.97.174:880", "188.114.96.76:878",
		"188.114.96.166:878", "188.114.96.206:878", "162.159.192.1:2408", "8.34.146.150:1701",
		"162.159.192.96:939",
	}
	if mode == "fast" {
		return fast, nil
	}
	if mode != "all" {
		return nil, fmt.Errorf("unknown scan mode %q; choose fast or all", mode)
	}
	subnets := []string{"188.114.96", "188.114.97", "162.159.195", "8.6.112"}
	ports := []int{2408, 500, 1701, 4500}
	all := make([]string, 0, len(subnets)*254*len(ports))
	for _, subnet := range subnets {
		for host := 1; host < 255; host++ {
			for _, port := range ports {
				all = append(all, net.JoinHostPort(subnet+"."+strconv.Itoa(host), strconv.Itoa(port)))
			}
		}
	}
	return all, nil
}

func scanEndpoints(cfg awgConfig, candidates []string, workers int, timeout time.Duration) ([]endpointHit, error) {
	return scanEndpointsWithProgress(cfg, candidates, workers, timeout, nil)
}

type scanProgress struct {
	done  int
	total int
	found int
}

func scanEndpointsWithProgress(cfg awgConfig, candidates []string, workers int, timeout time.Duration, report func(scanProgress)) ([]endpointHit, error) {
	if workers < 1 {
		workers = 1
	}
	if workers > 64 {
		workers = 64
	}
	if timeout < 100*time.Millisecond {
		timeout = 100 * time.Millisecond
	}
	if timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	jobs := make(chan string)
	hits := make([]endpointHit, 0)
	var mu sync.Mutex
	var progressMu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	var tested int
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				endpoint, err := normalizeEndpoint(candidate)
				if err == nil {
					started := time.Now()
					latency, ok, probeErr := probeEndpointConfig(cfg, endpoint, timeout)
					err = probeErr
					if ok {
						latency = time.Since(started)
						mu.Lock()
						hits = append(hits, endpointHit{endpoint, latency})
						mu.Unlock()
					}
				}
				if err != nil {
					errOnce.Do(func() { firstErr = err })
				}
				progressMu.Lock()
				mu.Lock()
				tested++
				state := scanProgress{done: tested, total: len(candidates), found: len(hits)}
				mu.Unlock()
				if report != nil {
					report(state)
				}
				progressMu.Unlock()
			}
		}()
	}
	for _, candidate := range candidates {
		jobs <- candidate
	}
	close(jobs)
	wg.Wait()
	if len(hits) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Latency < hits[j].Latency })
	return hits, nil
}
