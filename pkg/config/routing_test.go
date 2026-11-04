package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"0*", "0630123456", true},
		{"0*", "630123456", false},
		{"3612*", "3612345", true},
		{"*", "anything", true},
		{"1234", "1234", true},
		{"1234", "1235", false},
		{"1234", "12345", false},
		{"", "5", false},
	}
	for _, c := range cases {
		if got := MatchPattern(c.pattern, c.value); got != c.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", c.pattern, c.value, got, c.want)
		}
	}
}

func TestRoutesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gobelisk.yaml")
	yaml := `outbound_routes:
  - name: budapest
    pattern: "0*"
    trunk: uplink
    prefix_strip: 1
    cost: 10
    callerid: "+36125550001"
inbound_routes:
  - name: did-201
    did: "3612*"
    trunk: uplink
    destination: "201"
    cid_strip: 2
    cid_prefix: "+36"
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.OutboundRoutes) != 1 || len(cfg.InboundRoutes) != 1 {
		t.Fatalf("routes = %d/%d, want 1/1", len(cfg.OutboundRoutes), len(cfg.InboundRoutes))
	}
	out := cfg.OutboundRoutes[0]
	if out.Name != "budapest" || out.Trunk != "uplink" || out.PrefixStrip != 1 || out.Cost != 10 {
		t.Errorf("outbound route = %+v", out)
	}
	if out.CallerID != "+36125550001" {
		t.Errorf("outbound callerid = %q, want +36125550001", out.CallerID)
	}
	in := cfg.InboundRoutes[0]
	if in.DID != "3612*" || in.Trunk != "uplink" || in.Destination != "201" {
		t.Errorf("inbound route = %+v", in)
	}
	if in.CIDStrip != 2 || in.CIDPrefix != "+36" {
		t.Errorf("inbound cid manipulation = strip %d prefix %q, want 2 / +36", in.CIDStrip, in.CIDPrefix)
	}
}
