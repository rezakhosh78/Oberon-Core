package main

import (
	"strings"
	"testing"
)

func TestEndpointCandidatePoolsAndValidation(t *testing.T) {
	fast, err := endpointCandidates("fast")
	if err != nil {
		t.Fatal(err)
	}
	if len(fast) < 20 {
		t.Fatalf("fast pool unexpectedly small: %d", len(fast))
	}
	all, err := endpointCandidates("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4064 {
		t.Fatalf("all pool has %d candidates, want 4064", len(all))
	}
	if normalized, err := normalizeEndpoint("[2606:4700:100::1]:2408"); err != nil || normalized != "[2606:4700:100::1]:2408" {
		t.Fatalf("IPv6 endpoint normalization = %q, %v", normalized, err)
	}
	if _, err := normalizeEndpoint("hostname.example:2408"); err == nil {
		t.Fatal("hostname endpoint accepted; scanner candidates should be literal IPs")
	}
	if _, err := endpointCandidates("unknown"); err == nil {
		t.Fatal("unknown scan mode accepted")
	}
}

func TestReplaceEndpointPreservesOtherConfig(t *testing.T) {
	input := "[Interface]\nPrivateKey = key\nJc = 5\n\n[Peer]\nPublicKey = peer\nEndpoint = 192.0.2.1:2408\nAllowedIPs = 0.0.0.0/0\n"
	got := replacePeerEndpoint(input, "192.0.2.2:500")
	if got == input || !strings.Contains(got, "Jc = 5") || !strings.Contains(got, "Endpoint = 192.0.2.2:500") {
		t.Fatalf("endpoint update did not preserve config: %s", got)
	}
}
