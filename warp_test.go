package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

type warpRoundTripper func(*http.Request) (*http.Response, error)

func (f warpRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRegisterWarpParsesAccountAndRendersProfile(t *testing.T) {
	client := &http.Client{Transport: warpRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != warpRegistrationURL {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		}
		body := `{"client_id":"AQIDBA==","config":{"client_id":"AQIDBA==","interface":{"mtu":1280,"addresses":{"v4":"172.16.0.2/32","v6":"2606:4700:110:8e93::2/128"}},"peers":[{"public_key":"peer-key","endpoint":{"v4":"162.159.192.1","ports":[500,2408]}}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	profile, err := registerWarp(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Reserved) != 3 || profile.Reserved[0] != 1 || profile.Reserved[2] != 3 {
		t.Fatalf("unexpected reserved bytes: %v", profile.Reserved)
	}
	if profile.Endpoint != "162.159.192.1:2408" {
		t.Fatalf("unexpected endpoint: %s", profile.Endpoint)
	}
	if _, err := base64.StdEncoding.DecodeString(profile.PrivateKey); err != nil {
		t.Fatalf("private key is not base64: %v", err)
	}
	config := renderWarpConfig(profile, "")
	for _, expected := range []string{"[Interface]", "Jc = 5", "[Peer]", "AllowedIPs = 0.0.0.0/0, ::/0", "Endpoint = 162.159.192.1:2408"} {
		if !strings.Contains(config, expected) {
			t.Errorf("generated config lacks %q", expected)
		}
	}
}

func TestParseWarpPortSupportsSingleAndArrayValues(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int
	}{
		{`2408`, 2408},
		{`"500"`, 500},
		{`[500,2408]`, 2408},
		{`[500,4500]`, 500},
		{`[0,70000]`, 0},
	} {
		if got := parseWarpPort([]byte(test.input)); got != test.want {
			t.Errorf("parseWarpPort(%s) = %d, want %d", test.input, got, test.want)
		}
	}
}
