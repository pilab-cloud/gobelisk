package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTrunksYAML proves the durable store round-trips a provisioned trunk
// with defaults applied (GOBELISK-24).
func TestTrunksYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gobelisk.yaml")
	yaml := `trunks:
  - name: uplink
    type: pstn
    host: provider.example.com
    port: 5070
    username: gw
    password: s3cret
    register_expires: 120
    context: from-trunk
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Trunks) != 1 {
		t.Fatalf("trunks = %d, want 1", len(cfg.Trunks))
	}
	tr := cfg.Trunks[0]
	if tr.Name != "uplink" || tr.TrunkType() != "pstn" {
		t.Errorf("name/type = %q/%q", tr.Name, tr.TrunkType())
	}
	if tr.Host != "provider.example.com" || tr.PortOrDefault() != 5070 {
		t.Errorf("host/port = %q/%d", tr.Host, tr.PortOrDefault())
	}
	if tr.Password != "s3cret" || tr.Context != "from-trunk" {
		t.Errorf("password/context = %q/%q", tr.Password, tr.Context)
	}
	if !tr.RegisterEnabled() {
		t.Error("enabled should default to true")
	}
	if tr.ExpiresOrDefault() != 120 {
		t.Errorf("expires = %d, want 120", tr.ExpiresOrDefault())
	}
	if err := tr.Valid(); err != nil {
		t.Errorf("Valid: %v", err)
	}

	// Defaults on a minimal trunk.
	min := TrunkConfig{Name: "min", Host: "sip.example.com"}
	if min.TrunkType() != "sip" || min.PortOrDefault() != 5060 || min.ExpiresOrDefault() != 3600 {
		t.Errorf("defaults wrong: type=%q port=%d expires=%d",
			min.TrunkType(), min.PortOrDefault(), min.ExpiresOrDefault())
	}
	// An IAX trunk needs no host to be valid (protocol not implemented yet).
	if (TrunkConfig{Name: "iax", Type: "iax"}).Valid() != nil {
		t.Error("iax trunk without host should validate")
	}
	if (TrunkConfig{Name: "x", Type: "mgcp", Host: "h"}).Valid() == nil {
		t.Error("unknown type should not validate")
	}
	if (TrunkConfig{Host: "h"}).Valid() == nil {
		t.Error("nameless trunk should not validate")
	}
}
