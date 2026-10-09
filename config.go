package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/oberon-core/oberon/v1/conn"
	"github.com/oberon-core/oberon/v1/device"
	"github.com/oberon-core/oberon/v1/tun"
	"github.com/oberon-core/oberon/v1/tun/tuntest"
)

type awgConfig struct {
	iface map[string]string
	peer  map[string]string
}

func parseAWGConfig(path string) (awgConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return awgConfig{}, err
	}
	cfg := awgConfig{iface: map[string]string{}, peer: map[string]string{}}
	section := ""
	s := bufio.NewScanner(strings.NewReader(string(b)))
	for s.Scan() {
		line := strings.TrimSpace(strings.SplitN(s.Text(), "#", 2)[0])
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section != "interface" && section != "peer" {
				return awgConfig{}, fmt.Errorf("unsupported section [%s]", section)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			return awgConfig{}, fmt.Errorf("invalid config line: %s", line)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		target := cfg.iface
		if section == "peer" {
			target = cfg.peer
		}
		target[key] = value
	}
	if err := s.Err(); err != nil {
		return awgConfig{}, err
	}
	if cfg.iface["privatekey"] == "" || cfg.peer["publickey"] == "" || cfg.peer["endpoint"] == "" {
		return awgConfig{}, fmt.Errorf("config requires Interface.PrivateKey and Peer.PublicKey and Peer.Endpoint")
	}
	return cfg, nil
}

func keyHex(encoded string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("invalid 32-byte base64 key")
	}
	return hex.EncodeToString(key), nil
}

func (cfg awgConfig) uapi(endpoint string) (string, error) {
	private, err := keyHex(cfg.iface["privatekey"])
	if err != nil {
		return "", fmt.Errorf("interface private key: %w", err)
	}
	public, err := keyHex(cfg.peer["publickey"])
	if err != nil {
		return "", fmt.Errorf("peer public key: %w", err)
	}
	var lines []string
	add := func(key, value string) {
		if value != "" {
			lines = append(lines, key+"="+value)
		}
	}
	add("private_key", private)
	add("listen_port", cfg.iface["listenport"])
	add("fwmark", cfg.iface["fwmark"])
	for _, key := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4"} {
		add(key, cfg.iface[key])
	}
	add("replace_peers", "true")
	add("public_key", public)
	if psk := cfg.peer["presharedkey"]; psk != "" {
		pskHex, err := keyHex(psk)
		if err != nil {
			return "", fmt.Errorf("peer preshared key: %w", err)
		}
		add("preshared_key", pskHex)
	}
	if endpoint == "" {
		endpoint = cfg.peer["endpoint"]
	}
	add("endpoint", endpoint)
	allowed := strings.Split(cfg.peer["allowedips"], ",")
	for _, prefix := range allowed {
		if prefix = strings.TrimSpace(prefix); prefix != "" {
			add("allowed_ip", prefix)
		}
	}
	add("persistent_keepalive_interval", cfg.peer["persistentkeepalive"])
	return strings.Join(lines, "\n") + "\n\n", nil
}

func (cfg awgConfig) mtu() int {
	if n, err := strconv.Atoi(cfg.iface["mtu"]); err == nil && n >= 576 && n <= 9000 {
		return n
	}
	return 1280
}

func (cfg awgConfig) endpoint() string { return cfg.peer["endpoint"] }

func runConfig(path string) error {
	cfg, err := parseAWGConfig(path)
	if err != nil {
		return err
	}
	return runConfigProfile(cfg)
}

func runConfigProfile(cfg awgConfig) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return fmt.Errorf("configuration execution is currently supported on Linux and Windows; %s builds can generate and scan profiles", runtime.GOOS)
	}
	name := "oberon0"
	tdev, err := createSystemTUN(name, cfg.mtu())
	if err != nil {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("create Wintun TUN device (Oberon requests Administrator access automatically; ensure the matching Wintun 0.14.1 DLL is beside oberon.exe, using scripts\\setup-wintun.ps1 if needed): %w", err)
		}
		return fmt.Errorf("create TUN device (run with network-administrator privileges): %w", err)
	}
	name, err = tdev.Name()
	if err != nil {
		tdev.Close()
		return err
	}
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "(oberon) "))
	defer dev.Close()
	uapi, err := cfg.uapi("")
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" && hasDefaultRoute(cfg) {
		uapi = strings.TrimSuffix(uapi, "\n\n") + "\nfwmark=51820\n\n"
	}
	if err := dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("apply AmneziaWG profile: %w", err)
	}
	if err := dev.Up(); err != nil {
		return fmt.Errorf("start AmneziaWG device: %w", err)
	}
	var cleanup func()
	if runtime.GOOS == "windows" {
		cleanup, err = configureWindowsInterface(name, cfg)
	} else {
		cleanup, err = configureLinuxInterface(name, cfg)
	}
	if err != nil {
		return err
	}
	defer cleanup()
	fmt.Printf("Oberon tunnel active on %s. Press Ctrl+C to stop.\n", name)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()
	return nil
}

func createSystemTUN(name string, mtu int) (tdev tun.Device, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			tdev = nil
			err = fmt.Errorf("TUN driver panicked: %v", recovered)
			if info := loadedWintunDLLInfo(); info != "" {
				err = fmt.Errorf("%w; loaded Wintun DLL: %s", err, info)
			}
		}
	}()
	return tun.CreateTUN(name, mtu)
}

func configureLinuxInterface(name string, cfg awgConfig) (func(), error) {
	cleanupCommands := make([][]string, 0)
	run := func(args ...string) error {
		out, err := exec.Command("ip", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	cleanup := func() {
		for i := len(cleanupCommands) - 1; i >= 0; i-- {
			_ = exec.Command("ip", cleanupCommands[i]...).Run()
		}
	}
	rollback := func(err error) (func(), error) { cleanup(); return nil, err }
	if err := run("link", "set", "dev", name, "mtu", strconv.Itoa(cfg.mtu()), "up"); err != nil {
		return rollback(err)
	}
	cleanupCommands = append(cleanupCommands, []string{"link", "set", "dev", name, "down"})
	for _, address := range strings.Split(cfg.iface["address"], ",") {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		if err := run("address", "add", address, "dev", name); err != nil {
			return rollback(err)
		}
		cleanupCommands = append(cleanupCommands, []string{"address", "del", address, "dev", name})
	}
	allowed := strings.Split(cfg.peer["allowedips"], ",")
	for _, raw := range allowed {
		prefix := strings.TrimSpace(raw)
		if prefix == "" {
			continue
		}
		if prefix == "0.0.0.0/0" || prefix == "::/0" {
			continue
		}
		if err := run("route", "add", prefix, "dev", name); err != nil {
			return rollback(err)
		}
		cleanupCommands = append(cleanupCommands, []string{"route", "del", prefix, "dev", name})
	}
	fullV4, fullV6 := false, false
	for _, raw := range allowed {
		switch strings.TrimSpace(raw) {
		case "0.0.0.0/0": fullV4 = true
		case "::/0": fullV6 = true
		}
	}
	if fullV4 || fullV6 {
		const mark = "51820"
		for _, family := range []struct{ on bool; flag string }{{fullV4, "-4"}, {fullV6, "-6"}} {
			if !family.on { continue }
			if err := run(family.flag, "route", "add", "default", "dev", name, "table", mark); err != nil { return rollback(err) }
			cleanupCommands = append(cleanupCommands, []string{family.flag, "route", "flush", "table", mark})
			if err := run(family.flag, "rule", "add", "priority", "1000", "not", "fwmark", mark, "table", mark); err != nil { return rollback(err) }
			cleanupCommands = append(cleanupCommands, []string{family.flag, "rule", "del", "priority", "1000", "not", "fwmark", mark, "table", mark})
		}
	}
	if dns := strings.FieldsFunc(cfg.iface["dns"], func(r rune) bool { return r == ',' || r == ' ' }); len(dns) > 0 {
		if _, err := exec.LookPath("resolvectl"); err == nil {
			if err := exec.Command("resolvectl", append([]string{"dns", name}, dns...)...).Run(); err == nil {
				cleanupCommands = append(cleanupCommands, []string{"resolvectl", "revert", name})
			}
		}
	}
	return cleanup, nil
}

func configureWindowsInterface(name string, cfg awgConfig) (func(), error) {
	cleanupScripts := make([]string, 0)
	run := func(script string) (string, error) {
		out, err := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("PowerShell command failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	cleanup := func() {
		for i := len(cleanupScripts) - 1; i >= 0; i-- {
			_, _ = run(cleanupScripts[i])
		}
	}
	rollback := func(err error) (func(), error) { cleanup(); return nil, err }
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

	indexOutput, err := run(fmt.Sprintf("$ErrorActionPreference='Stop'; (Get-NetAdapter -Name %s -ErrorAction Stop).ifIndex", quote(name)))
	if err != nil {
		return nil, fmt.Errorf("find Wintun interface (Windows denied access after elevation): %w", err)
	}
	tunIndex, err := strconv.Atoi(indexOutput)
	if err != nil || tunIndex < 1 {
		return nil, fmt.Errorf("invalid Wintun interface index %q", indexOutput)
	}

	for _, raw := range strings.Split(cfg.iface["address"], ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		ip, prefix, err := net.ParseCIDR(raw)
		if err != nil {
			return rollback(fmt.Errorf("invalid interface address %q: %w", raw, err))
		}
		prefixBits, _ := prefix.Mask.Size()
		family := "IPv6"
		if ip.To4() != nil {
			family = "IPv4"
			ip = ip.To4()
		}
		script := fmt.Sprintf("$ErrorActionPreference='Stop'; New-NetIPAddress -InterfaceIndex %d -AddressFamily %s -IPAddress %s -PrefixLength %d -ErrorAction Stop | Out-Null", tunIndex, family, quote(ip.String()), prefixBits)
		if _, err := run(script); err != nil {
			return rollback(fmt.Errorf("assign %s to Wintun (Windows denied access after elevation): %w", raw, err))
		}
		cleanupScripts = append(cleanupScripts, fmt.Sprintf("$ErrorActionPreference='Continue'; Remove-NetIPAddress -InterfaceIndex %d -IPAddress %s -Confirm:$false -ErrorAction SilentlyContinue", tunIndex, quote(ip.String())))
	}

	allowedRoutes := make([]string, 0)
	fullV4, fullV6 := false, false
	for _, raw := range strings.Split(cfg.peer["allowedips"], ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, prefix, err := net.ParseCIDR(raw)
		if err != nil {
			return rollback(fmt.Errorf("invalid allowed IP prefix %q: %w", raw, err))
		}
		bits, _ := prefix.Mask.Size()
		if bits == 0 && prefix.IP.To4() != nil {
			fullV4 = true
			allowedRoutes = append(allowedRoutes, "0.0.0.0/1", "128.0.0.0/1")
			continue
		}
		if bits == 0 {
			fullV6 = true
			allowedRoutes = append(allowedRoutes, "::/1", "8000::/1")
			continue
		}
		allowedRoutes = append(allowedRoutes, prefix.String())
	}

	// Add physical host routes for the peer before installing tunnel default
	// routes. Otherwise the peer's own UDP packets could be routed back into
	// the tunnel and prevent the handshake from staying alive.
	if fullV4 || fullV6 {
		endpointHost, _, err := net.SplitHostPort(cfg.peer["endpoint"])
		if err != nil {
			return rollback(fmt.Errorf("invalid peer endpoint %q: %w", cfg.peer["endpoint"], err))
		}
		endpointIPs := []net.IP{net.ParseIP(strings.Trim(endpointHost, "[]"))}
		if endpointIPs[0] == nil {
			endpointIPs, err = net.LookupIP(endpointHost)
			if err != nil {
				return rollback(fmt.Errorf("resolve peer endpoint %q before routing: %w", endpointHost, err))
			}
		}
		if len(endpointIPs) == 0 {
			return rollback(fmt.Errorf("peer endpoint %q did not resolve to an IP address", endpointHost))
		}
		for _, endpointIP := range endpointIPs {
			family, destination, enabled := "IPv4", endpointIP.To4(), fullV4
			if endpointIP.To4() == nil {
				family, destination, enabled = "IPv6", endpointIP, fullV6
			}
			if !enabled {
				continue
			}
			prefixBits := 32
			if family == "IPv6" {
				prefixBits = 128
			}
			defaultPrefix := "0.0.0.0/0"
			if family == "IPv6" {
				defaultPrefix = "::/0"
			}
			routeOutput, err := run(fmt.Sprintf("$ErrorActionPreference='Stop'; $r=Get-NetRoute -AddressFamily %s -DestinationPrefix %s -ErrorAction Stop | Sort-Object RouteMetric | Select-Object -First 1; if($null -eq $r){throw 'No physical default route'}; Write-Output (\"{0}|{1}\" -f $r.InterfaceIndex,$r.NextHop)", family, quote(defaultPrefix)))
			if err != nil {
				return rollback(fmt.Errorf("find physical %s route for WARP endpoint: %w", family, err))
			}
			parts := strings.SplitN(routeOutput, "|", 2)
			if len(parts) != 2 {
				return rollback(fmt.Errorf("unexpected physical route result %q", routeOutput))
			}
			physicalIndex, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil || physicalIndex < 1 {
				return rollback(fmt.Errorf("invalid physical interface index in %q", routeOutput))
			}
			endpointPrefix := fmt.Sprintf("%s/%d", destination.String(), prefixBits)
			addRoute := fmt.Sprintf("$ErrorActionPreference='Stop'; $old=Get-NetRoute -AddressFamily %s -DestinationPrefix %s -ErrorAction SilentlyContinue | Where-Object {$_.InterfaceIndex -ne %d} | Select-Object -First 1; if($null -eq $old){New-NetRoute -AddressFamily %s -DestinationPrefix %s -InterfaceIndex %d -NextHop %s -RouteMetric 1 -PolicyStore ActiveStore -ErrorAction Stop | Out-Null; Write-Output 'added'}else{Write-Output 'existing'}", family, quote(endpointPrefix), tunIndex, family, quote(endpointPrefix), physicalIndex, quote(parts[1]))
			result, err := run(addRoute)
			if err != nil {
				return rollback(fmt.Errorf("preserve physical route to WARP endpoint %s: %w", endpointPrefix, err))
			}
			if result == "added" {
				cleanupScripts = append(cleanupScripts, fmt.Sprintf("$ErrorActionPreference='Continue'; Remove-NetRoute -AddressFamily %s -DestinationPrefix %s -InterfaceIndex %d -Confirm:$false -ErrorAction SilentlyContinue", family, quote(endpointPrefix), physicalIndex))
			}
		}
	}

	routeSeen := make(map[string]bool)
	for _, route := range allowedRoutes {
		if routeSeen[route] {
			continue
		}
		routeSeen[route] = true
		family, nextHop := "IPv4", "0.0.0.0"
		if strings.Contains(route, ":") {
			family, nextHop = "IPv6", "::"
		}
		script := fmt.Sprintf("$ErrorActionPreference='Stop'; New-NetRoute -AddressFamily %s -DestinationPrefix %s -InterfaceIndex %d -NextHop %s -RouteMetric 1 -PolicyStore ActiveStore -ErrorAction Stop | Out-Null", family, quote(route), tunIndex, quote(nextHop))
		if _, err := run(script); err != nil {
			return rollback(fmt.Errorf("add Wintun route %s (Windows denied access after elevation): %w", route, err))
		}
		cleanupScripts = append(cleanupScripts, fmt.Sprintf("$ErrorActionPreference='Continue'; Remove-NetRoute -AddressFamily %s -DestinationPrefix %s -InterfaceIndex %d -Confirm:$false -ErrorAction SilentlyContinue", family, quote(route), tunIndex))
	}

	var dns []string
	for _, server := range strings.FieldsFunc(cfg.iface["dns"], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if net.ParseIP(server) == nil {
			return rollback(fmt.Errorf("invalid DNS server address %q", server))
		}
		dns = append(dns, quote(server))
	}
	if len(dns) > 0 {
		script := fmt.Sprintf("$ErrorActionPreference='Stop'; Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses @(%s) -ErrorAction Stop", tunIndex, strings.Join(dns, ","))
		if _, err := run(script); err != nil {
			return rollback(fmt.Errorf("set Wintun DNS servers (Windows denied access after elevation): %w", err))
		}
		cleanupScripts = append(cleanupScripts, fmt.Sprintf("$ErrorActionPreference='Continue'; Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses -ErrorAction SilentlyContinue", tunIndex))
	}
	return cleanup, nil
}

func hasDefaultRoute(cfg awgConfig) bool {
	for _, raw := range strings.Split(cfg.peer["allowedips"], ",") {
		if prefix := strings.TrimSpace(raw); prefix == "0.0.0.0/0" || prefix == "::/0" { return true }
	}
	return false
}

func probeEndpointConfig(cfg awgConfig, candidate string, timeout time.Duration) (time.Duration, bool, error) {
	// A handshake probe sends and receives WireGuard transport packets only;
	// it does not need an operating-system network interface or administrator
	// privileges. An in-memory TUN keeps scanning portable and avoids creating
	// a Wintun adapter for every candidate on Windows.
	tdev := tuntest.NewChannelTUN().TUN()
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
	defer dev.Close()
	if err := dev.Up(); err != nil { return 0, false, err }
	uapi, err := cfg.uapi(candidate)
	if err != nil { return 0, false, err }
	started := time.Now()
	uapi = strings.TrimSuffix(uapi, "\n\n") + "\npersistent_keepalive_interval=1\n\n"
	if err := dev.IpcSet(uapi); err != nil { return 0, false, err }
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := dev.IpcGet()
		if err != nil { return 0, false, err }
		if strings.Contains(state, "last_handshake_time_sec=0\n") == false {
			return time.Since(started), true, nil
		}
		time.Sleep(35 * time.Millisecond)
	}
	return 0, false, nil
}

func normalizeEndpoint(raw string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(raw))
	if err != nil { return "", err }
	if net.ParseIP(host) == nil { return "", fmt.Errorf("endpoint host must be an IP address: %q", host) }
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 { return "", fmt.Errorf("invalid endpoint port %q", port) }
	return net.JoinHostPort(host, port), nil
}
