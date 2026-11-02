package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const warpRegistrationURL = "https://api.cloudflareclient.com/v0a1922/reg"

// newWarpHTTPClient falls back to Cloudflare DNS-over-HTTPS if the system
// resolver cannot resolve the WARP registration hostname.
func newWarpHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = warpDialContext
	return &http.Client{Timeout: timeout, Transport: transport}
}

func warpDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	if net.ParseIP(host) != nil {
		return dialer.DialContext(ctx, network, address)
	}
	resolved, systemErr := net.DefaultResolver.LookupIPAddr(ctx, host)
	if systemErr != nil || len(resolved) == 0 {
		resolved, err = lookupWarpDoHIPs(ctx, host)
		if err != nil {
			if systemErr != nil {
				return nil, fmt.Errorf("system DNS failed (%v); Cloudflare DoH fallback failed: %w", systemErr, err)
			}
			return nil, fmt.Errorf("Cloudflare DoH fallback failed: %w", err)
		}
	}
	var lastErr error
	for _, ip := range resolved {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("DNS returned no IP addresses")
	}
	return nil, lastErr
}

func lookupWarpDoHIPs(ctx context.Context, host string) ([]net.IPAddr, error) {
	var lastErr error
	for _, bootstrapIP := range []string{"1.1.1.1", "1.0.0.1"} {
		var ips []net.IPAddr
		for _, qtype := range []string{"A", "AAAA"} {
			answers, err := queryWarpDoH(ctx, bootstrapIP, host, qtype)
			if err != nil {
				lastErr = err
				continue
			}
			ips = append(ips, answers...)
		}
		if len(ips) > 0 {
			return ips, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no A or AAAA records returned")
	}
	return nil, lastErr
}

func queryWarpDoH(ctx context.Context, bootstrapIP, host, qtype string) ([]net.IPAddr, error) {
	values := url.Values{"name": {host}, "type": {qtype}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cloudflare-dns.com/dns-query?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/dns-json")
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: "cloudflare-dns.com", MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(bootstrapIP, port))
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("DoH returned %s", response.Status)
	}
	var result struct {
		Status int
		Answer []struct {
			Type int
			Data string
		}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, err
	}
	if result.Status != 0 {
		return nil, fmt.Errorf("DNS query returned status %d", result.Status)
	}
	var ips []net.IPAddr
	for _, answer := range result.Answer {
		ip := net.ParseIP(strings.TrimSpace(answer.Data))
		if ip == nil || (qtype == "A" && ip.To4() == nil) || (qtype == "AAAA" && ip.To4() != nil) {
			continue
		}
		ips = append(ips, net.IPAddr{IP: ip})
	}
	return ips, nil
}

type warpProfile struct {
	PrivateKey    string
	Address       string
	PeerPublicKey string
	Endpoint      string
	MTU           int
	Reserved      []byte
}

type warpRegistration struct {
	Config struct {
		ClientID  string `json:"client_id"`
		Interface struct {
			MTU       int `json:"mtu"`
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
		Peers []struct {
			PublicKey string          `json:"public_key"`
			Endpoint  json.RawMessage `json:"endpoint"`
			Port      json.RawMessage `json:"port"`
			Ports     json.RawMessage `json:"ports"`
		} `json:"peers"`
	} `json:"config"`
	ClientID string `json:"client_id"`
}

func registerWarp(ctx context.Context, client *http.Client) (warpProfile, error) {
	curve := ecdh.X25519()
	private, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return warpProfile{}, fmt.Errorf("generate X25519 key: %w", err)
	}
	payload, err := json.Marshal(map[string]string{
		"key":        base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()),
		"install_id": "",
		"fcm_token":  "",
		"tos":        time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"model":      "Oberon",
		"locale":     "en_US",
		"type":       "Linux",
	})
	if err != nil {
		return warpProfile{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, warpRegistrationURL, bytes.NewReader(payload))
	if err != nil {
		return warpProfile{}, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "Oberon/1.0")
	req.Header.Set("CF-Client-Version", "a-6.3-1922")
	resp, err := client.Do(req)
	if err != nil {
		return warpProfile{}, fmt.Errorf("WARP registration request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return warpProfile{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return warpProfile{}, fmt.Errorf("WARP registration failed (%s): %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var body warpRegistration
	if err := json.Unmarshal(data, &body); err != nil {
		return warpProfile{}, fmt.Errorf("decode WARP registration response: %w", err)
	}
	if len(body.Config.Peers) == 0 {
		return warpProfile{}, errors.New("WARP registration returned no peer")
	}
	iface := body.Config.Interface
	v4 := strings.TrimSuffix(strings.TrimSpace(iface.Addresses.V4), "/32")
	v6 := strings.TrimSuffix(strings.TrimSpace(iface.Addresses.V6), "/128")
	if net.ParseIP(v4) == nil {
		return warpProfile{}, errors.New("WARP registration returned an invalid IPv4 address")
	}
	addresses := v4 + "/32"
	if v6 != "" {
		if net.ParseIP(v6) == nil {
			return warpProfile{}, errors.New("WARP registration returned an invalid IPv6 address")
		}
		addresses += ", " + v6 + "/128"
	}
	peer := body.Config.Peers[0]
	if peer.PublicKey == "" {
		return warpProfile{}, errors.New("WARP registration returned no peer key")
	}
	var endpoint string
	var endpointPorts json.RawMessage
	if len(peer.Endpoint) != 0 && peer.Endpoint[0] == '"' {
		if err := json.Unmarshal(peer.Endpoint, &endpoint); err != nil {
			return warpProfile{}, fmt.Errorf("decode WARP peer endpoint: %w", err)
		}
	} else if len(peer.Endpoint) != 0 {
		var endpoints map[string]json.RawMessage
		if err := json.Unmarshal(peer.Endpoint, &endpoints); err != nil {
			return warpProfile{}, fmt.Errorf("decode WARP peer endpoint: %w", err)
		}
		endpoint = decodeWarpEndpointAddress(endpoints["v4"])
		if endpoint == "" {
			endpoint = decodeWarpEndpointAddress(endpoints["v6"])
		}
		endpointPorts = endpoints["ports"]
	}
	host, embeddedPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		// Cloudflare may return an IP without an embedded port.
		host = strings.Trim(endpoint, "[]")
		if net.ParseIP(host) == nil {
			return warpProfile{}, errors.New("WARP registration returned an invalid endpoint")
		}
	}
	port := parseWarpPort(peer.Port)
	if port == 0 {
		port = parseWarpPort(peer.Ports)
	}
	if port == 0 {
		port = parseWarpPort(endpointPorts)
	}
	if port == 0 && embeddedPort != "" {
		port, _ = strconv.Atoi(embeddedPort)
	}
	if port == 0 {
		port = 2408
	}
	if port < 1 || port > 65535 {
		return warpProfile{}, errors.New("WARP registration returned an invalid endpoint port")
	}
	if strings.Contains(host, ":") {
		endpoint = net.JoinHostPort(host, strconv.Itoa(port))
	} else {
		endpoint = net.JoinHostPort(host, strconv.Itoa(port))
	}
	clientID := body.Config.ClientID
	if clientID == "" {
		clientID = body.ClientID
	}
	reserved, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(clientID, "="))
	if err != nil || len(reserved) < 3 {
		reserved, err = base64.StdEncoding.DecodeString(clientID)
	}
	if err != nil || len(reserved) < 3 {
		return warpProfile{}, errors.New("WARP registration returned an invalid client identifier")
	}
	mtu := iface.MTU
	if mtu == 0 {
		mtu = 1280
	}
	return warpProfile{
		PrivateKey: base64.StdEncoding.EncodeToString(private.Bytes()), Address: addresses,
		PeerPublicKey: peer.PublicKey, Endpoint: endpoint, MTU: mtu, Reserved: reserved[:3],
	}, nil
}

func decodeWarpEndpointAddress(raw json.RawMessage) string {
	var address string
	if len(raw) == 0 || json.Unmarshal(raw, &address) != nil {
		return ""
	}
	return strings.TrimSpace(address)
}

// WARP responses have used both a single port and a list of available ports.
// Accept either representation so an API response shape change cannot prevent
// account creation.
func parseWarpPort(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var single int
	if err := json.Unmarshal(raw, &single); err == nil {
		return validWarpPort(single)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value, _ := strconv.Atoi(strings.TrimSpace(text))
		return validWarpPort(value)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		first := 0
		for _, item := range list {
			value := parseWarpPort(item)
			if value == 2408 {
				return value
			}
			if first == 0 && value != 0 {
				first = value
			}
		}
		return first
	}
	return 0
}

func validWarpPort(port int) int {
	if port < 1 || port > 65535 {
		return 0
	}
	return port
}

func renderWarpConfig(profile warpProfile, endpoint string) string {
	if endpoint == "" {
		endpoint = profile.Endpoint
	}
	return fmt.Sprintf("# Generated by Oberon\n[Interface]\nPrivateKey = %s\nAddress = %s\nDNS = 1.1.1.1, 1.0.0.1, 2606:4700:4700::1111, 2606:4700:4700::1001\nMTU = %d\nJc = 5\nJmin = 10\nJmax = 40\nS1 = 0\nS2 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = %s\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = %s\n",
		profile.PrivateKey, profile.Address, profile.MTU, profile.PeerPublicKey, endpoint)
}
