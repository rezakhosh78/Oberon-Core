package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/oberon-core/oberon/v1/conn"
	"github.com/oberon-core/oberon/v1/device"
	"github.com/oberon-core/oberon/v1/tun/netstack"
)

func validateLocalProxyAddr(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("expected host:port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if host == "localhost" {
		return nil
	}
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil || !ip.IsLoopback() {
		return fmt.Errorf("bind to a loopback address such as 127.0.0.1 or [::1] to keep the proxy private")
	}
	return nil
}

func runProxySession(cfg awgConfig, socksAddress, httpAddress string) error {
	stopSpinner := startTerminalSpinner("Starting Oberon with AmneziaWG WARP")
	spinnerActive := true
	defer func() {
		if spinnerActive {
			stopSpinner()
		}
	}()
	localAddresses, dnsServers, err := proxyNetworkAddresses(cfg)
	if err != nil {
		return err
	}
	tdev, tnet, err := netstack.CreateNetTUN(localAddresses, dnsServers, cfg.mtu())
	if err != nil {
		return fmt.Errorf("create userspace tunnel: %w", err)
	}
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "(oberon) "))
	defer dev.Close()
	keepalive := cfg.peer["persistentkeepalive"]
	cfg.peer["persistentkeepalive"] = "1"
	uapi, err := cfg.uapi(cfg.endpoint())
	if err != nil {
		return err
	}
	if err := dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("apply AmneziaWG profile: %w", err)
	}
	if err := dev.Up(); err != nil {
		return fmt.Errorf("start AmneziaWG tunnel: %w", err)
	}
	if err := waitForTunnelHandshake(dev, 10*time.Second); err != nil {
		return err
	}
	if keepalive == "" {
		keepalive = "25"
	}
	publicKey, err := keyHex(cfg.peer["publickey"])
	if err != nil {
		return fmt.Errorf("peer public key: %w", err)
	}
	if err := dev.IpcSet(fmt.Sprintf("public_key=%s\npersistent_keepalive_interval=%s\n\n", publicKey, keepalive)); err != nil {
		return fmt.Errorf("set peer keepalive: %w", err)
	}

	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	if socksAddress != "" {
		listener, err := net.Listen("tcp", socksAddress)
		if err != nil {
			return fmt.Errorf("listen for SOCKS5 proxy on %s: %w", socksAddress, err)
		}
		listeners = append(listeners, listener)
		socksAddress = listener.Addr().String()
	}
	var httpServer *http.Server
	if httpAddress != "" {
		listener, err := net.Listen("tcp", httpAddress)
		if err != nil {
			return fmt.Errorf("listen for HTTP proxy on %s: %w", httpAddress, err)
		}
		listeners = append(listeners, listener)
		httpAddress = listener.Addr().String()
		httpServer = &http.Server{Handler: newHTTPProxyHandler(tnet), ReadHeaderTimeout: 10 * time.Second}
	}

	serveErrors := make(chan error, 2)
	if socksAddress != "" {
		listener := listeners[0]
		go func() { serveErrors <- serveSOCKS5(listener, tnet) }()
	}
	if httpServer != nil {
		listener := listeners[len(listeners)-1]
		go func() { serveErrors <- httpServer.Serve(listener) }()
	}
	stopSpinner()
	spinnerActive = false
	printProxyConnectionInfo(cfg.endpoint(), socksAddress, httpAddress)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	select {
	case <-ctx.Done():
	case serveErr := <-serveErrors:
		if serveErr != nil && serveErr != http.ErrServerClosed && !strings.Contains(strings.ToLower(serveErr.Error()), "use of closed network connection") {
			return fmt.Errorf("proxy listener stopped: %w", serveErr)
		}
	}
	if httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}
	return nil
}

func printProxyConnectionInfo(endpoint, socksAddress, httpAddress string) {
	drawTUIFrameWithStatus("CONNECTED", "AmneziaWG WARP tunnel is active.", socksAddress, httpAddress, "Oberon Connected", func(width int) {
		if endpoint != "" {
			printTUIWrapped(width, "Connected endpoint: "+endpoint, "1;37")
		}
		if socksAddress != "" || httpAddress != "" {
			printTUIRow(width, "Proxy authentication: none", "2")
			printTUIRow(width, "Press Ctrl+C to disconnect", "2")
		}
	})
}

func waitForTunnelHandshake(dev *device.Device, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := dev.IpcGet()
		if err != nil {
			return fmt.Errorf("check AmneziaWG handshake: %w", err)
		}
		if !strings.Contains(state, "last_handshake_time_sec=0\n") {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("AmneziaWG tunnel did not establish a handshake after the endpoint scan")
}

func proxyNetworkAddresses(cfg awgConfig) ([]netip.Addr, []netip.Addr, error) {
	var addresses []netip.Addr
	for _, raw := range strings.FieldsFunc(cfg.iface["address"], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid interface address %q: %w", raw, err)
		}
		addresses = append(addresses, prefix.Addr())
	}
	if len(addresses) == 0 {
		return nil, nil, fmt.Errorf("proxy mode requires an Interface.Address in the AWG profile")
	}
	var dnsServers []netip.Addr
	for _, raw := range strings.FieldsFunc(cfg.iface["dns"], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		ip, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid Interface.DNS server %q: %w", raw, err)
		}
		dnsServers = append(dnsServers, ip)
	}
	if len(dnsServers) == 0 {
		return nil, nil, fmt.Errorf("proxy mode needs DNS servers in the AWG profile (for example, DNS = 1.1.1.1)")
	}
	return addresses, dnsServers, nil
}

func serveSOCKS5(listener net.Listener, dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}) error {
	for {
		client, err := listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Err != nil {
				if strings.Contains(strings.ToLower(opErr.Err.Error()), "closed network connection") {
					return nil
				}
			}
			return err
		}
		go handleSOCKS5(client, dialer)
	}
}

func handleSOCKS5(client net.Conn, dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(client)
	version, err := reader.ReadByte()
	if err != nil || version != 5 {
		return
	}
	methodCount, err := reader.ReadByte()
	if err != nil {
		return
	}
	methods := make([]byte, int(methodCount))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	noAuth := false
	for _, method := range methods {
		if method == 0 {
			noAuth = true
			break
		}
	}
	if !noAuth {
		_, _ = client.Write([]byte{5, 0xff})
		return
	}
	if _, err := client.Write([]byte{5, 0}); err != nil {
		return
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(reader, request); err != nil {
		return
	}
	if request[0] != 5 || request[1] != 1 || request[2] != 0 {
		writeSOCKSReply(client, 7)
		return
	}
	host, err := readSOCKSHost(reader, request[3])
	if err != nil {
		writeSOCKSReply(client, 8)
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	if port == 0 {
		writeSOCKSReply(client, 1)
		return
	}
	_ = client.SetDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	upstream, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	cancel()
	if err != nil {
		writeSOCKSReply(client, 5)
		return
	}
	defer upstream.Close()
	if err := writeSOCKSReply(client, 0); err != nil {
		return
	}
	proxyCopy(client, upstream)
}

func readSOCKSHost(reader io.Reader, addressType byte) (string, error) {
	switch addressType {
	case 1:
		address := make([]byte, 4)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", err
		}
		return net.IP(address).String(), nil
	case 3:
		length, err := readByte(reader)
		if err != nil || length == 0 {
			return "", fmt.Errorf("invalid SOCKS5 domain name")
		}
		name := make([]byte, int(length))
		if _, err := io.ReadFull(reader, name); err != nil {
			return "", err
		}
		return string(name), nil
	case 4:
		address := make([]byte, 16)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", err
		}
		return net.IP(address).String(), nil
	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type")
	}
}

func readByte(reader io.Reader) (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(reader, b[:])
	return b[0], err
}

func writeSOCKSReply(writer io.Writer, code byte) error {
	_, err := writer.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}

func proxyCopy(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		_ = upstream.Close()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		done <- struct{}{}
	}()
	<-done
}

func newHTTPProxyHandler(dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}) http.Handler {
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   false,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Method, http.MethodConnect) {
			handleHTTPConnect(w, r, dialer)
			return
		}
		if r.URL == nil || (r.URL.Scheme != "http" && r.URL.Scheme != "https") {
			http.Error(w, "proxy request must use an absolute http:// or https:// URL", http.StatusBadRequest)
			return
		}
		outgoing := r.Clone(r.Context())
		outgoing.RequestURI = ""
		outgoing.Header = r.Header.Clone()
		outgoing.Header.Del("Proxy-Authorization")
		outgoing.Header.Del("Proxy-Connection")
		response, err := transport.RoundTrip(outgoing)
		if err != nil {
			http.Error(w, "upstream request failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})
}

func handleHTTPConnect(w http.ResponseWriter, r *http.Request, dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}) {
	target := r.Host
	if _, _, err := net.SplitHostPort(target); err != nil {
		if strings.Contains(target, ":") && !strings.HasPrefix(target, "[") {
			http.Error(w, "invalid CONNECT target", http.StatusBadRequest)
			return
		}
		target = net.JoinHostPort(strings.Trim(target, "[]"), "443")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	upstream, err := dialer.DialContext(ctx, "tcp", target)
	cancel()
	if err != nil {
		http.Error(w, "upstream connection failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "HTTP connection hijacking is unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := buffered.Flush(); err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return
	}
	go func() {
		_, _ = io.Copy(upstream, buffered.Reader)
		_ = upstream.Close()
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	_ = upstream.Close()
}
