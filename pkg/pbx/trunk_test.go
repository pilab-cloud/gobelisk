package pbx

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devopshaven/gobelisk/gen/go/gobelisk/v1"
	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/rs/zerolog"
)

// ---- harness ----

// startTrunkServer is startServer for trunk tests: it also returns the
// server handle so the tests can inspect trunk status.
func startTrunkServer(t *testing.T, cfg *config.Config) (*Server, *net.UDPAddr) {
	t.Helper()
	port := freeUDPPort(t)
	s := NewServer(WithConfig(cfg), WithLogger(zerolog.Nop()))
	ready := make(chan struct{})
	s.onListen = func() { close(ready) }
	go func() { _ = s.ListenUDP(port) }()
	t.Cleanup(func() { _ = s.Close() })

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start listening")
	}
	return s, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
}

// startRegistrar runs a fake SIP provider: handler returns the response to
// send ("" to ignore the request).
func startRegistrar(t *testing.T, handler func(req string) string) *testUA {
	t.Helper()
	ua := newTestUA(t)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		buf := make([]byte, 65535)
		for {
			ua.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, from, err := ua.conn.ReadFromUDP(buf)
			if err != nil {
				select {
				case <-stop:
					return
				default:
					continue
				}
			}
			if resp := handler(string(buf[:n])); resp != "" {
				_, _ = ua.conn.WriteToUDP([]byte(resp), from)
			}
		}
	}()
	return ua
}

// respTo builds a provider response with extra headers and an optional body.
func respTo(req string, code int, reason string, extra map[string]string, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, reason)
	fmt.Fprintf(&b, "Via: %s\r\n", headerValue(req, "Via"))
	fmt.Fprintf(&b, "From: %s\r\n", headerValue(req, "From"))
	to := headerValue(req, "To")
	if !strings.Contains(to, "tag=") {
		to += ";tag=prov"
	}
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", headerValue(req, "Call-ID"))
	fmt.Fprintf(&b, "CSeq: %s\r\n", headerValue(req, "CSeq"))
	for k, v := range extra {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	if body != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n", len(body))
	b.WriteString(body)
	return b.String()
}

func waitTrunk(t *testing.T, s *Server, name, want string, timeout time.Duration) trunkStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last trunkStatus
	known := false
	for time.Now().Before(deadline) {
		last, known = s.trunkStatus(name)
		if known && last.State == want {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("trunk %q never reached %q (known=%v state=%q alert=%q failures=%d)",
		name, want, known, last.State, last.Alert, last.Failures)
	return trunkStatus{}
}

func waitTrunkGone(t *testing.T, s *Server, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, ok := s.trunkStatus(name); !ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("trunk %q still present after %s", name, timeout)
}

// ---- registration and health (GOBELISK-25) ----

func TestTrunkRegistersWithDigestChallenge(t *testing.T) {
	var plain, authed int32
	authCh := make(chan string, 4)
	registrar := startRegistrar(t, func(req string) string {
		if !strings.HasPrefix(req, "REGISTER") {
			return ""
		}
		if headerValue(req, "Authorization") == "" {
			atomic.AddInt32(&plain, 1)
			return respTo(req, 401, "Unauthorized", map[string]string{
				"WWW-Authenticate": `Digest realm="prov.example.com", nonce="nonce1", algorithm=MD5`,
			}, "")
		}
		atomic.AddInt32(&authed, 1)
		authCh <- headerValue(req, "Authorization")
		return respTo(req, 200, "OK", map[string]string{"Expires": "120"}, "")
	})

	cfg := config.Default()
	cfg.Trunks = []config.TrunkConfig{{
		Name:     "uplink",
		Host:     "127.0.0.1",
		Port:     registrar.addr.Port,
		Username: "gw",
		Password: "secret",
	}}
	server, _ := startTrunkServer(t, cfg)

	st := waitTrunk(t, server, "uplink", TrunkStateRegistered, 5*time.Second)

	// The digest retry carried real credentials.
	select {
	case auth := <-authCh:
		if !strings.Contains(auth, `username="gw"`) {
			t.Errorf("Authorization username wrong: %s", auth)
		}
		if !strings.Contains(auth, `response="`) {
			t.Errorf("Authorization missing response: %s", auth)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("digest retry never arrived")
	}
	if atomic.LoadInt32(&plain) != 1 {
		t.Errorf("plain REGISTER count = %d, want 1", plain)
	}
	if atomic.LoadInt32(&authed) != 1 {
		t.Errorf("authenticated REGISTER count = %d, want 1", authed)
	}

	// Status is shown with no failures and a live schedule.
	if st.Failures != 0 || st.Alert != "" {
		t.Errorf("fresh trunk shows failures=%d alert=%q", st.Failures, st.Alert)
	}
	if st.LastRegister.IsZero() || st.ExpiresAt.IsZero() {
		t.Errorf("fresh trunk missing register/expiry timestamps: %+v", st)
	}

	// The password never leaves the server.
	list, err := server.ListTrunks(context.Background(), &gobeliskv1.ListTrunksRequest{})
	if err != nil {
		t.Fatalf("ListTrunks: %v", err)
	}
	if len(list.Trunks) != 1 {
		t.Fatalf("ListTrunks len = %d, want 1", len(list.Trunks))
	}
	got := list.Trunks[0]
	if got.Password != "" {
		t.Errorf("ListTrunks leaked the password: %q", got.Password)
	}
	if got.State != TrunkStateRegistered || got.Type != "sip" {
		t.Errorf("listed trunk = type %q state %q", got.Type, got.State)
	}
	if got.LastRegisterUnix == 0 || got.ExpiresUnix == 0 {
		t.Errorf("listed trunk missing status timestamps: %+v", got)
	}
}

func TestTrunkRegisterRenews(t *testing.T) {
	var registers int32
	registrar := startRegistrar(t, func(req string) string {
		if !strings.HasPrefix(req, "REGISTER") {
			return ""
		}
		atomic.AddInt32(&registers, 1)
		return respTo(req, 200, "OK", map[string]string{"Expires": "4"}, "")
	})

	cfg := config.Default()
	cfg.Trunks = []config.TrunkConfig{{
		Name:            "short",
		Host:            "127.0.0.1",
		Port:            registrar.addr.Port,
		RegisterExpires: 4, // renewed at half life: ~2s
	}}
	server, _ := startTrunkServer(t, cfg)

	waitTrunk(t, server, "short", TrunkStateRegistered, 5*time.Second)

	deadline := time.Now().Add(6 * time.Second)
	for atomic.LoadInt32(&registers) < 2 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if n := atomic.LoadInt32(&registers); n < 2 {
		t.Errorf("registers = %d, want at least 2 (initial + renewal)", n)
	}
}

func TestTrunkAuthFailureRaisesAlert(t *testing.T) {
	registrar := startRegistrar(t, func(req string) string {
		if !strings.HasPrefix(req, "REGISTER") {
			return ""
		}
		return respTo(req, 403, "Forbidden", nil, "")
	})

	cfg := config.Default()
	cfg.Trunks = []config.TrunkConfig{{
		Name:         "denied",
		Host:         "127.0.0.1",
		Port:         registrar.addr.Port,
		Username:     "gw",
		Password:     "wrong",
		RetrySeconds: 1,
	}}
	server, _ := startTrunkServer(t, cfg)

	// Three consecutive failures flip it to unhealthy with an alert.
	st := waitTrunk(t, server, "denied", TrunkStateUnhealthy, 8*time.Second)
	if st.Failures < 3 {
		t.Errorf("failures = %d, want >= 3", st.Failures)
	}
	if !strings.Contains(st.Alert, "403") {
		t.Errorf("alert %q should carry the failure cause", st.Alert)
	}
	if st.LastError == "" || !strings.Contains(st.LastError, "403") {
		t.Errorf("lastError = %q, want the 403", st.LastError)
	}
}

func TestTrunkHealthCheckRuns(t *testing.T) {
	optsCh := make(chan struct{}, 8)
	registrar := startRegistrar(t, func(req string) string {
		switch {
		case strings.HasPrefix(req, "REGISTER"):
			return respTo(req, 200, "OK", map[string]string{"Expires": "60"}, "")
		case strings.HasPrefix(req, "OPTIONS"):
			select {
			case optsCh <- struct{}{}:
			default:
			}
			return respTo(req, 200, "OK", nil, "")
		}
		return ""
	})

	cfg := config.Default()
	cfg.Trunks = []config.TrunkConfig{{
		Name:                  "probed",
		Host:                  "127.0.0.1",
		Port:                  registrar.addr.Port,
		HealthIntervalSeconds: 1,
	}}
	server, _ := startTrunkServer(t, cfg)

	waitTrunk(t, server, "probed", TrunkStateRegistered, 5*time.Second)

	select {
	case <-optsCh:
	case <-time.After(4 * time.Second):
		t.Fatal("no OPTIONS health probe arrived at the provider")
	}
	// A successful probe keeps the trunk healthy.
	st := waitTrunk(t, server, "probed", TrunkStateRegistered, 3*time.Second)
	if st.Failures != 0 || st.Alert != "" {
		t.Errorf("healthy trunk shows failures=%d alert=%q", st.Failures, st.Alert)
	}
}

// ---- provisioning (GOBELISK-24) ----

func TestTrunkProvisioningRPC(t *testing.T) {
	registrar := startRegistrar(t, func(req string) string {
		if strings.HasPrefix(req, "REGISTER") {
			return respTo(req, 200, "OK", map[string]string{"Expires": "60"}, "")
		}
		return ""
	})
	ctx := context.Background()
	server, _ := startTrunkServer(t, config.Default())

	// A disabled PSTN trunk: provisioned, no registration attempted.
	if _, err := server.CreateTrunk(ctx, &gobeliskv1.CreateTrunkRequest{Trunk: &gobeliskv1.Trunk{
		Name: "pstn1", Type: "pstn", Host: "127.0.0.1", Port: int32(registrar.addr.Port),
		Username: "gw", Password: "x", Enabled: false,
	}}); err != nil {
		t.Fatalf("CreateTrunk(pstn): %v", err)
	}
	// An IAX trunk: provisioned, protocol not implemented -> unsupported.
	if _, err := server.CreateTrunk(ctx, &gobeliskv1.CreateTrunkRequest{Trunk: &gobeliskv1.Trunk{
		Name: "iax1", Type: "iax", Enabled: true,
	}}); err != nil {
		t.Fatalf("CreateTrunk(iax): %v", err)
	}
	// A SIP trunk provisioned over RPC registers immediately.
	if _, err := server.CreateTrunk(ctx, &gobeliskv1.CreateTrunkRequest{Trunk: &gobeliskv1.Trunk{
		Name: "gw", Type: "sip", Host: "127.0.0.1", Port: int32(registrar.addr.Port),
		Username: "gw", Password: "secret", Enabled: true,
	}}); err != nil {
		t.Fatalf("CreateTrunk(sip): %v", err)
	}

	// Duplicates are rejected.
	if _, err := server.CreateTrunk(ctx, &gobeliskv1.CreateTrunkRequest{Trunk: &gobeliskv1.Trunk{
		Name: "gw", Type: "sip", Host: "127.0.0.1", Enabled: true,
	}}); err == nil {
		t.Error("duplicate trunk create succeeded")
	}
	// Unknown types are rejected.
	if _, err := server.CreateTrunk(ctx, &gobeliskv1.CreateTrunkRequest{Trunk: &gobeliskv1.Trunk{
		Name: "weird", Type: "mgcp", Host: "127.0.0.1", Enabled: true,
	}}); err == nil {
		t.Error("unknown trunk type accepted")
	}

	waitTrunk(t, server, "gw", TrunkStateRegistered, 5*time.Second)

	list, err := server.ListTrunks(ctx, &gobeliskv1.ListTrunksRequest{})
	if err != nil {
		t.Fatalf("ListTrunks: %v", err)
	}
	byName := map[string]*gobeliskv1.Trunk{}
	for _, tr := range list.Trunks {
		byName[tr.Name] = tr
	}
	if got := byName["pstn1"]; got == nil || got.Type != "pstn" || got.State != TrunkStateDisabled {
		t.Errorf("pstn1 = %+v, want type pstn state disabled", got)
	}
	if got := byName["iax1"]; got == nil || got.Type != "iax" || got.State != TrunkStateUnsupported {
		t.Errorf("iax1 = %+v, want type iax state unsupported", got)
	}
	if got := byName["gw"]; got == nil || got.State != TrunkStateRegistered {
		t.Errorf("gw = %+v, want registered", got)
	}

	// Update applies (and re-registers with the changed config).
	if _, err := server.UpdateTrunk(ctx, &gobeliskv1.UpdateTrunkRequest{Trunk: &gobeliskv1.Trunk{
		Name: "gw", Type: "sip", Host: "127.0.0.1", Port: int32(registrar.addr.Port),
		Username: "gw", Enabled: true, Callerid: "Sales",
	}}); err != nil {
		t.Fatalf("UpdateTrunk: %v", err)
	}
	waitTrunk(t, server, "gw", TrunkStateRegistered, 5*time.Second)
	list, _ = server.ListTrunks(ctx, &gobeliskv1.ListTrunksRequest{})
	for _, tr := range list.Trunks {
		if tr.Name == "gw" && tr.Callerid != "Sales" {
			t.Errorf("callerid not updated: %q", tr.Callerid)
		}
		if tr.Name == "gw" && tr.Password != "" {
			t.Errorf("password leaked after update: %q", tr.Password)
		}
	}

	// Remove stops and forgets the trunk.
	if _, err := server.RemoveTrunk(ctx, &gobeliskv1.RemoveTrunkRequest{Name: "gw"}); err != nil {
		t.Fatalf("RemoveTrunk: %v", err)
	}
	waitTrunkGone(t, server, "gw", 3*time.Second)
	if _, err := server.RemoveTrunk(ctx, &gobeliskv1.RemoveTrunkRequest{Name: "nope"}); err == nil {
		t.Error("removing an unknown trunk succeeded")
	}
}
