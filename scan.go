package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
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
	family := fs.String("family", "ipv4", "endpoint address family: ipv4 or ipv6")
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
		return fmt.Errorf("usage: oberon scan -p awg -P -conf profile.conf [-o output.conf] [-mode fast|all] [-family ipv4|ipv6]")
	}
	cfg, err := parseAWGConfig(*configPath)
	if err != nil {
		return err
	}
	scanFamily := strings.ToLower(strings.TrimSpace(*family))
	candidates, err := endpointCandidatesForFamily(*mode, scanFamily)
	if err != nil {
		return err
	}
	if err := ensureScanFamilyAvailable(scanFamily); err != nil {
		return err
	}
	familyName := strings.ToUpper(scanFamily)
	fmt.Fprintf(os.Stderr, "Scanning %s endpoints (%d candidates)...\n", familyName, len(candidates))
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
	return endpointCandidatesForFamily(mode, "ipv4")
}

// The built-in pools follow WARPSCOUT's WARP WireGuard/AmneziaWG pools.
// IPv6 /48 pools are sampled because enumerating them is not practical.
var warpIPv4Pools = []string{
	"8.6.112.0/24", "8.34.70.0/24", "8.34.146.0/24", "8.35.211.0/24",
	"8.39.125.0/24", "8.39.204.0/24", "8.39.214.0/24", "8.47.69.0/24",
	"162.159.192.0/24", "162.159.195.0/24",
	"188.114.96.0/24", "188.114.97.0/24", "188.114.98.0/24", "188.114.99.0/24",
}

var warpIPv6Pools = []string{
	"2606:4700:d0::/48", "2606:4700:d1::/48",
}

var warpScanPorts = []int{2408, 500, 1701, 4500}

func endpointCandidatesForFamily(mode, family string) ([]string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	family = strings.ToLower(strings.TrimSpace(family))
	if mode != "fast" && mode != "all" {
		return nil, fmt.Errorf("unknown scan mode %q; choose fast or all", mode)
	}
	if family != "ipv4" && family != "ipv6" {
		return nil, fmt.Errorf("unknown IP family %q; choose ipv4 or ipv6", family)
	}

	legacyFastIPv4 := []string{
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

	var pools []string
	if family == "ipv4" {
		pools = warpIPv4Pools
	} else {
		pools = warpIPv6Pools
	}

	perPool := 5
	if mode == "all" {
		if family == "ipv4" {
			perPool = 254
		} else {
			// WARPSCOUT's full IPv6 scan samples 256 addresses per /48.
			perPool = 256
		}
	}

	ports := []int{2408}
	if mode == "all" {
		ports = warpScanPorts
	}
	candidates := make([]string, 0, len(pools)*perPool*len(ports)+len(legacyFastIPv4))
	if family == "ipv4" {
		candidates = append(candidates, legacyFastIPv4...)
	}

	for _, pool := range pools {
		prefix, err := netip.ParsePrefix(pool)
		if err != nil {
			return nil, fmt.Errorf("invalid built-in endpoint pool %q: %w", pool, err)
		}
		addresses := samplePoolAddresses(prefix, perPool)
		for _, address := range addresses {
			for _, port := range ports {
				candidates = append(candidates, net.JoinHostPort(address.String(), strconv.Itoa(port)))
			}
		}
	}
	return uniqueEndpoints(candidates), nil
}

func ensureScanFamilyAvailable(family string) error {
	family = strings.ToLower(strings.TrimSpace(family))
	if family != "ipv4" && family != "ipv6" {
		return fmt.Errorf("unknown IP family %q; choose ipv4 or ipv6", family)
	}
	network, endpoint := "udp4", "188.114.96.1:2408"
	if family == "ipv6" {
		network, endpoint = "udp6", "[2606:4700:d0::1]:2408"
	}
	connection, err := net.DialTimeout(network, endpoint, 2*time.Second)
	if err != nil {
		return fmt.Errorf("no routable %s address on this device: %w", family, err)
	}
	return connection.Close()
}

func samplePoolAddresses(prefix netip.Prefix, count int) []netip.Addr {
	prefix = prefix.Masked()
	addresses := make([]netip.Addr, 0, count)
	if prefix.Addr().Is4() {
		order := rand.Perm(254)
		if count < len(order) {
			order = order[:count]
		}
		base := prefix.Addr().As4()
		for _, offset := range order {
			address := base
			address[3] = byte(offset + 1)
			addresses = append(addresses, netip.AddrFrom4(address))
		}
		return addresses
	}

	base := prefix.Addr().As16()
	seen := make(map[uint64]struct{}, count)
	for len(addresses) < count {
		host := rand.Uint64()
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		address := base
		binary.BigEndian.PutUint64(address[8:], host)
		addresses = append(addresses, netip.AddrFrom16(address))
	}
	return addresses
}

func uniqueEndpoints(candidates []string) []string {
	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
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
