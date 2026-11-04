package pbx

import (
	"strings"
	"testing"
	"time"

	"github.com/devopshaven/gobelisk/pkg/config"
)

// recvCh waits for one message on a channel.
func recvCh(t *testing.T, ch <-chan string, what string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatalf("%s never arrived", what)
		return ""
	}
}

// requestURI returns the Request-URI of a request line ("INVITE sip:...").
func requestURI(msg string) string {
	if fields := strings.Fields(firstLine(msg)); len(fields) >= 2 {
		return fields[1]
	}
	return ""
}

// ---- outbound routes (GOBELISK-21) ----

func TestOutboundRouteCallPlaced(t *testing.T) {
	inviteCh := make(chan string, 4)
	ackCh := make(chan string, 4)
	byeCh := make(chan string, 4)
	var provider *testUA
	provider = startRegistrar(t, func(req string) string {
		switch {
		case strings.HasPrefix(req, "REGISTER"):
			return respTo(req, 200, "OK", map[string]string{"Expires": "60"}, "")
		case strings.HasPrefix(req, "INVITE"):
			inviteCh <- req
			return respTo(req, 200, "OK", map[string]string{
				"Contact": "<sip:outbound@" + provider.addr.String() + ">",
			}, "")
		case strings.HasPrefix(req, "ACK "):
			ackCh <- req
		case strings.HasPrefix(req, "BYE "):
			byeCh <- req
		}
		return ""
	})

	cfg := config.Default()
	cfg.Trunks = []config.TrunkConfig{{
		Name: "uplink", Host: "127.0.0.1", Port: provider.addr.Port,
		Username: "gw", Password: "x",
	}}
	cfg.OutboundRoutes = []config.OutboundRoute{{
		Name: "local", Pattern: "0*", Trunk: "uplink", PrefixStrip: 1,
	}}
	srv, srvAddr := startTrunkServer(t, cfg)
	waitTrunk(t, srv, "uplink", TrunkStateRegistered, 5*time.Second)

	caller := newTestUA(t)
	caller.send(invite("out-1", "100", "0630123456", caller.addr, srvAddr), srvAddr)

	// Pattern matched, prefix stripped, trunk selected: the provider sees
	// the number without the "0" trunk access code.
	inv := recvCh(t, inviteCh, "INVITE at the trunk")
	if uri := requestURI(inv); !strings.HasPrefix(uri, "sip:630123456@") {
		t.Errorf("outbound Request-URI = %q, want sip:630123456@...", uri)
	}
	if !strings.Contains(headerValue(inv, "From"), "100") {
		t.Errorf("original caller lost in From: %s", headerValue(inv, "From"))
	}

	// The call placed: the answer comes back to the caller...
	caller.expect("SIP/2.0 200", 2*time.Second)

	// ... and the dialog continues through the trunk: ACK and BYE land there.
	caller.send(ackOrBye("ACK", "out-1", "100", "630123456", caller.addr, srvAddr, 1), srvAddr)
	ack := recvCh(t, ackCh, "ACK at the trunk")
	if !strings.HasPrefix(ack, "ACK ") {
		t.Errorf("expected ACK, got %q", firstLine(ack))
	}

	caller.send(ackOrBye("BYE", "out-1", "100", "630123456", caller.addr, srvAddr, 2), srvAddr)
	recvCh(t, byeCh, "BYE at the trunk")
	caller.expect("SIP/2.0 200", 2*time.Second)
}

func TestOutboundINVITEDigestRetry(t *testing.T) {
	noAuthCh := make(chan string, 4)
	authCh := make(chan string, 4)
	var provider *testUA
	provider = startRegistrar(t, func(req string) string {
		switch {
		case strings.HasPrefix(req, "REGISTER"):
			return respTo(req, 200, "OK", map[string]string{"Expires": "60"}, "")
		case strings.HasPrefix(req, "INVITE"):
			if headerValue(req, "Authorization") == "" {
				noAuthCh <- req
				return respTo(req, 401, "Unauthorized", map[string]string{
					"WWW-Authenticate": `Digest realm="prov.example.com", nonce="n2", algorithm=MD5`,
				}, "")
			}
			authCh <- req
			return respTo(req, 200, "OK", map[string]string{
				"Contact": "<sip:outbound@" + provider.addr.String() + ">",
			}, "")
		}
		return ""
	})

	cfg := config.Default()
	cfg.Trunks = []config.TrunkConfig{{
		Name: "uplink", Host: "127.0.0.1", Port: provider.addr.Port,
		Username: "gw", Password: "secret",
	}}
	cfg.OutboundRoutes = []config.OutboundRoute{{
		Name: "any", Pattern: "*", Trunk: "uplink",
	}}
	srv, srvAddr := startTrunkServer(t, cfg)
	waitTrunk(t, srv, "uplink", TrunkStateRegistered, 5*time.Second)

	caller := newTestUA(t)
	caller.send(invite("out-2", "100", "12345", caller.addr, srvAddr), srvAddr)

	// The trunk challenges; the server retries with credentials.
	recvCh(t, noAuthCh, "first INVITE at the trunk")
	authed := recvCh(t, authCh, "authenticated INVITE at the trunk")
	if !strings.Contains(headerValue(authed, "Authorization"), `username="gw"`) {
		t.Errorf("retry missing digest credentials: %s", headerValue(authed, "Authorization"))
	}
	if !strings.Contains(headerValue(authed, "Authorization"), `response="`) {
		t.Errorf("retry missing digest response: %s", headerValue(authed, "Authorization"))
	}

	// The caller never sees the challenge — only its own transaction's
	// responses — and ends up connected.
	deadline := time.Now().Add(3 * time.Second)
	saw401, connected := false, false
	for time.Now().Before(deadline) && !connected {
		msg := caller.recv(time.Until(deadline))
		if msg == "" {
			break
		}
		line := firstLine(msg)
		if strings.Contains(line, "SIP/2.0 401") {
			saw401 = true
		}
		if strings.Contains(line, "SIP/2.0 200") {
			connected = true
		}
	}
	if saw401 {
		t.Error("the trunk's 401 leaked to the caller")
	}
	if !connected {
		t.Error("caller never received the 200")
	}
}

// ---- least-cost routing (GOBELISK-22) ----

func TestOutboundLeastCostRouting(t *testing.T) {
	cheapInvite := make(chan string, 4)
	expensiveInvite := make(chan string, 4)

	var cheap, expensive *testUA
	cheap = startRegistrar(t, func(req string) string {
		if strings.HasPrefix(req, "REGISTER") {
			return respTo(req, 200, "OK", map[string]string{"Expires": "60"}, "")
		}
		if strings.HasPrefix(req, "INVITE") {
			cheapInvite <- req
			return respTo(req, 200, "OK", map[string]string{
				"Contact": "<sip:cheap@" + cheap.addr.String() + ">",
			}, "")
		}
		return ""
	})
	expensive = startRegistrar(t, func(req string) string {
		if strings.HasPrefix(req, "REGISTER") {
			return respTo(req, 200, "OK", map[string]string{"Expires": "60"}, "")
		}
		if strings.HasPrefix(req, "INVITE") {
			expensiveInvite <- req
			return respTo(req, 200, "OK", map[string]string{
				"Contact": "<sip:exp@" + expensive.addr.String() + ">",
			}, "")
		}
		return ""
	})

	cfg := config.Default()
	cfg.Trunks = []config.TrunkConfig{
		{Name: "trunkA", Host: "127.0.0.1", Port: expensive.addr.Port, Username: "a"},
		{Name: "trunkB", Host: "127.0.0.1", Port: cheap.addr.Port, Username: "b"},
	}
	cfg.OutboundRoutes = []config.OutboundRoute{
		{Name: "expensive", Pattern: "1*", Trunk: "trunkA", Cost: 10},
		{Name: "cheap", Pattern: "1*", Trunk: "trunkB", Cost: 5},
	}
	srv, srvAddr := startTrunkServer(t, cfg)
	waitTrunk(t, srv, "trunkA", TrunkStateRegistered, 5*time.Second)
	waitTrunk(t, srv, "trunkB", TrunkStateRegistered, 5*time.Second)

	caller := newTestUA(t)
	caller.send(invite("lcr-1", "100", "1999", caller.addr, srvAddr), srvAddr)

	// Both routes match; the cheaper one is ranked first and dialed.
	inv := recvCh(t, cheapInvite, "INVITE at the cheap trunk")
	if uri := requestURI(inv); !strings.HasPrefix(uri, "sip:1999@") {
		t.Errorf("cheap trunk Request-URI = %q", uri)
	}
	select {
	case got := <-expensiveInvite:
		t.Errorf("expensive trunk also dialed: %s", firstLine(got))
	case <-time.After(500 * time.Millisecond):
	}
	caller.expect("SIP/2.0 200", 2*time.Second)
}

// ---- inbound routes (GOBELISK-20) ----

func TestInboundRouteDIDReachesDestination(t *testing.T) {
	provider := newTestUA(t) // the source of inbound calls, configured as trunk
	ext201 := newTestUA(t)

	cfg := config.Default()
	// The trunk must match the provider's exact source address.
	cfg.Trunks = []config.TrunkConfig{{
		Name: "in", Host: "127.0.0.1", Port: provider.addr.Port,
		Enabled: config.Bool(false), // no registration traffic needed
	}}
	cfg.InboundRoutes = []config.InboundRoute{{
		Name: "did201", DID: "3612*", Trunk: "in", Destination: "201",
	}}
	srv, srvAddr := startTrunkServer(t, cfg)
	// The trunk is provisioned (and disabled): it only serves as the
	// inbound source marker here.
	waitTrunk(t, srv, "in", TrunkStateDisabled, 3*time.Second)
	ext201.register("201", srvAddr)

	// DID matched: the destination rings and the answer returns to the
	// trunk that carried the call.
	provider.send(invite("inb-1", "3615556666", "361234567", provider.addr, srvAddr), srvAddr)
	inv := ext201.expect("INVITE", 2*time.Second)
	if !strings.Contains(headerValue(inv, "From"), "3615556666") {
		t.Errorf("upstream caller lost in From: %s", headerValue(inv, "From"))
	}
	ext201.send(respond(inv, 200, "OK", "201", ext201), srvAddr)
	provider.expect("SIP/2.0 200", 2*time.Second)

	// An unknown DID from the trunk is rejected locally: it must never
	// hairpin back out through an outbound route.
	provider.send(invite("inb-2", "3615556666", "9999999", provider.addr, srvAddr), srvAddr)
	provider.expect("SIP/2.0 404", 2*time.Second)
	provider.expectNone(400 * time.Millisecond)
}
