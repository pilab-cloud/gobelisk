package pbx

import (
	"strings"
	"testing"
	"time"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
)

// ---- Do Not Disturb (GOBELISK-14) ----

func TestDNDRejectsIncomingCall(t *testing.T) {
	cfg := config.Default()
	cfg.Features.DNDExtensions = []string{"201"}
	server := startServer(t, cfg, dialplan.New())

	callee := newTestUA(t)
	callee.register("201", server)

	caller := newTestUA(t)
	caller.send(invite("dnd-1", "100", "201", caller.addr, server), server)
	resp := caller.expect("SIP/2.0 603", 2*time.Second)
	if !strings.Contains(firstLine(resp), "603") {
		t.Errorf("want 603 Declined, got %q", firstLine(resp))
	}
	// The DND extension is never disturbed.
	callee.expectNone(400 * time.Millisecond)
}

func TestDNDDisabledCallConnects(t *testing.T) {
	cfg := config.Default()                      // no dnd_extensions configured
	cfg.Features.DNDExtensions = []string{"999"} // some other extension
	server := startServer(t, cfg, dialplan.New())

	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("201", server)
	establishCall(t, server, caller, callee, "dnd-off", "100", "201")
}

func TestRingGroupSkipsDNDMember(t *testing.T) {
	cfg := config.Default()
	cfg.Features.RingTimeoutSeconds = 3
	cfg.Features.DNDExtensions = []string{"101"}
	server := startServer(t, cfg, ringGroupDialplan(t, "ringgroup 600 -> 101 102\n"))

	ua101 := newTestUA(t)
	ua102 := newTestUA(t)
	ua101.register("101", server)
	ua102.register("102", server)

	caller := newTestUA(t)
	caller.send(invite("dnd-rg", "100", "600", caller.addr, server), server)
	caller.expect("100 Trying", 2*time.Second)

	// 101 has DND on: only 102 may ring.
	ua102.expect("INVITE", 2*time.Second)
	ua101.expectNone(400 * time.Millisecond)
}

// ---- call forwarding (GOBELISK-15) ----

func TestCallForwardUnconditional(t *testing.T) {
	cfg := config.Default()
	cfg.Features.CallForward = map[string]config.ForwardConfig{
		"201": {Unconditional: "302"},
	}
	server := startServer(t, cfg, dialplan.New())

	forwarded := newTestUA(t) // 302 gets every call
	original := newTestUA(t)  // 201 must never ring
	forwarded.register("302", server)
	original.register("201", server)

	caller := newTestUA(t)
	caller.send(invite("cfu-1", "100", "201", caller.addr, server), server)

	inv := forwarded.expect("INVITE", 2*time.Second)
	if headerValue(inv, "To") == "" {
		t.Errorf("forwarded INVITE missing To header")
	}
	original.expectNone(400 * time.Millisecond)

	forwarded.send(respond(inv, 200, "OK", "302", forwarded), server)
	caller.expect("SIP/2.0 200", 2*time.Second)
}

func TestCallForwardBusy(t *testing.T) {
	cfg := config.Default()
	cfg.Features.CallWaiting = false
	cfg.Features.CallForward = map[string]config.ForwardConfig{
		"201": {Busy: "302"},
	}
	server := startServer(t, cfg, dialplan.New())

	callee := newTestUA(t)   // 201: busy with the first call
	fallback := newTestUA(t) // 302: receives forwarded calls
	callee.register("201", server)
	fallback.register("302", server)

	// First call occupies 201.
	caller1 := newTestUA(t)
	caller1.send(invite("cfb-1", "100", "201", caller1.addr, server), server)
	inv1 := callee.expect("INVITE", 2*time.Second)
	callee.send(respond(inv1, 200, "OK", "201", callee), server)
	caller1.expect("SIP/2.0 200", 2*time.Second)
	caller1.send(ackOrBye("ACK", "cfb-1", "100", "201", caller1.addr, server, 1), server)
	callee.expect("ACK", 2*time.Second)

	// Second call is busy-forwarded to 302 instead of rejected 486.
	caller2 := newTestUA(t)
	caller2.send(invite("cfb-2", "101", "201", caller2.addr, server), server)

	inv2 := fallback.expect("INVITE", 2*time.Second)
	callee.expectNone(400 * time.Millisecond) // 201 not disturbed
	fallback.send(respond(inv2, 200, "OK", "302", fallback), server)
	caller2.expect("SIP/2.0 200", 2*time.Second)
}

func TestCallForwardNoAnswer(t *testing.T) {
	cfg := config.Default()
	cfg.Features.CallForward = map[string]config.ForwardConfig{
		"701": {NoAnswer: "702", NoAnswerSeconds: 1},
	}
	server := startServer(t, cfg, dialplan.New())

	rang := newTestUA(t) // 701: rings, never picks up
	fwd := newTestUA(t)  // 702: the no-answer target
	rang.register("701", server)
	fwd.register("702", server)

	caller := newTestUA(t)
	caller.send(invite("cfna-1", "100", "701", caller.addr, server), server)
	caller.expect("100 Trying", 2*time.Second)

	inv701 := rang.expect("INVITE", 2*time.Second)

	// After the configured ring time the unanswered leg is cancelled on the
	// INVITE's own Via branch, as RFC 3261 requires for CANCEL.
	cancel := rang.expect("CANCEL", 4*time.Second)
	if b1, b2 := topViaBranch(headerValue(inv701, "Via")), topViaBranch(headerValue(cancel, "Via")); b1 != b2 {
		t.Errorf("CANCEL branch %q does not match INVITE branch %q", b2, b1)
	}
	rang.send(respond(inv701, 487, "Request Terminated", "701", rang), server)

	// The forward target rings instead; the caller only ever sees success.
	inv702 := fwd.expect("INVITE", 3*time.Second)
	fwd.send(respond(inv702, 200, "OK", "702", fwd), server)

	resp := caller.expect("SIP/2.0 200", 2*time.Second)
	if v := headerValue(resp, "Call-ID"); v != "cfna-1" {
		t.Errorf("forwarded call lost its Call-ID: %q", v)
	}
	caller.send(ackOrBye("ACK", "cfna-1", "100", "702", caller.addr, server, 1), server)
	fwd.expect("ACK", 2*time.Second)
}

func TestCallForwardLoopLimit(t *testing.T) {
	cfg := config.Default()
	cfg.Features.CallForward = map[string]config.ForwardConfig{
		"801": {Unconditional: "801"}, // misconfigured self-loop
	}
	server := startServer(t, cfg, dialplan.New())

	looped := newTestUA(t)
	looped.register("801", server)

	caller := newTestUA(t)
	caller.send(invite("cfl-1", "100", "801", caller.addr, server), server)
	resp := caller.expect("SIP/2.0 508", 3*time.Second)
	if !strings.Contains(firstLine(resp), "508") {
		t.Errorf("want 508 Loop Detected, got %q", firstLine(resp))
	}
	// The loop must not hammer the extension.
	looped.expectNone(300 * time.Millisecond)
}
