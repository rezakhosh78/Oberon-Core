package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyWarpProfileToConfigReplacesIdentityAndPreservesAWGOptions(t *testing.T) {
	input := `[Interface]
PrivateKey = old-private
Address = 172.16.0.2/32
MTU = 1420
Jc = 5
Jmin = 10

[Peer]
PublicKey = old-public
PresharedKey = old-preshared
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 162.159.192.1:2408
`
	profile := warpProfile{
		PrivateKey:    "new-private",
		Address:       "172.16.0.3/32, 2606:4700::3/128",
		PeerPublicKey: "new-public",
		Endpoint:      "162.159.192.2:2408",
		MTU:           1280,
	}

	got, err := applyWarpProfileToConfig(input, profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"PrivateKey = new-private",
		"Address = 172.16.0.3/32, 2606:4700::3/128",
		"MTU = 1280",
		"Jc = 5",
		"PublicKey = new-public",
		"AllowedIPs = 0.0.0.0/0, ::/0",
		"Endpoint = 162.159.192.2:2408",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("updated profile lacks %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "PresharedKey") {
		t.Errorf("obsolete preshared key was retained:\n%s", got)
	}
}

func TestDiscoverAWGProfilesInSearchesOnlyValidConfigs(t *testing.T) {
	directory := t.TempDir()
	valid := `[Interface]
PrivateKey = private
Address = 172.16.0.2/32
[Peer]
PublicKey = public
Endpoint = 162.159.192.1:2408
`
	if err := os.WriteFile(filepath.Join(directory, "warp-awg.conf"), []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unrelated.conf"), []byte("not a tunnel profile"), 0600); err != nil {
		t.Fatal(err)
	}
	profiles := discoverAWGProfilesIn([]string{directory})
	if len(profiles) != 1 || filepath.Base(profiles[0]) != "warp-awg.conf" {
		t.Fatalf("discovered profiles = %v, want only warp-awg.conf", profiles)
	}
}
