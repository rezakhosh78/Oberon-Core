package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigBuildsAmneziaWGControlConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.conf")
	profile := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 172.16.0.2/32
MTU = 1280
Jc = 5
Jmin = 10
Jmax = 40

[Peer]
PublicKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 162.159.192.1:2408
`
	if err := os.WriteFile(path, []byte(profile), 0600); err != nil { t.Fatal(err) }
	cfg, err := parseAWGConfig(path)
	if err != nil { t.Fatal(err) }
	uapi, err := cfg.uapi("")
	if err != nil { t.Fatal(err) }
	for _, expected := range []string{"jc=5", "jmin=10", "jmax=40", "allowed_ip=0.0.0.0/0", "endpoint=162.159.192.1:2408"} {
		if !strings.Contains(uapi, expected) { t.Errorf("control config lacks %q", expected) }
	}
}
