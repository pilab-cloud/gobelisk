package pbx

import (
	"strings"
	"testing"
	"time"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
)

// ---- feature codes (GOBELISK-18) ----

func TestFeatureDNDOnOff(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())

	subject := newTestUA(t) // 100: dials the codes, becomes DND
	other := newTestUA(t)   // 201: calls 100 afterwards
	subject.register("100", server)
	other.register("201", server)

	// *78 enables DND for the calling extension: answered as a dialog the
	// server then hangs up (no media path for confirmation tones).
	subject.send(invite("fc-1", "100", "*78", subject.addr, server), server)
	resp := subject.expect("SIP/2.0 200", 2*time.Second)
	if !strings.Contains(headerValue(resp, "To"), "tag=") {
		t.Errorf("feature answer has no To-tag:\n%s", resp)
	}
	if c := headerValue(resp, "Contact"); !strings.Contains(c, "sip:server@") {
		t.Errorf("feature answer Contact = %q, want the server", c)
	}
	// The server ends the feature call shortly after.
	subject.expect("BYE", 3*time.Second)

	// Incoming calls to the DND'd extension are declined...
	caller := newTestUA(t)
	caller.send(invite("fc-2", "201", "100", caller.addr, server), server)
	caller.expect("SIP/2.0 603", 2*time.Second)

	// ... until *79 turns DND off again.
	subject.send(invite("fc-3", "100", "*79", subject.addr, server), server)
	subject.expect("SIP/2.0 200", 2*time.Second)
	subject.expect("BYE", 3*time.Second)

	caller.send(invite("fc-4", "201", "100", caller.addr, server), server)
	inv := subject.expect("INVITE", 2*time.Second)
	if !strings.Contains(inv, "INVITE") {
		t.Errorf("expected ringing after DND off")
	}
}

func TestFeatureRequiresRegisteredCaller(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())

	stranger := newTestUA(t) // never registers
	stranger.send(invite("fc-reg", "999", "*78", stranger.addr, server), server)
	resp := stranger.expect("SIP/2.0 403", 2*time.Second)
	if !strings.Contains(firstLine(resp), "403") {
		t.Errorf("want 403 for unregistered feature caller, got %q", firstLine(resp))
	}
}

func TestFeatureUnknownCodeFallsThrough(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())

	caller := newTestUA(t)
	caller.register("100", server)
	caller.send(invite("fc-unk", "100", "*99", caller.addr, server), server)
	// Not a configured code: routed as a number (nobody owns *99 -> 404).
	caller.expect("SIP/2.0 404", 2*time.Second)
}

// ---- call parking (GOBELISK-12) ----

func TestParkAndPickupFlow(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())

	parker := newTestUA(t) // 100 parks the call
	picker := newTestUA(t) // 300 picks it up
	parker.register("100", server)
	picker.register("300", server)

	// Park via *70: the server answers (inactive media) and keeps the call.
	// Real phones always offer SDP, so does this one.
	parker.send(dialogReq("INVITE", "pk-1", "100", "from-pk-1", "*70", "", 1, parker, server, sdpFor("sendrecv")), server)
	resp := parker.expect("SIP/2.0 200", 2*time.Second)
	if !strings.Contains(headerValue(resp, "To"), "tag=") {
		t.Errorf("park answer has no To-tag:\n%s", resp)
	}
	if !strings.Contains(resp, "a=inactive") {
		t.Errorf("park answer must be media-inactive:\n%s", resp)
	}
	parker.send(ackOrBye("ACK", "pk-1", "100", "701", parker.addr, server, 1), server)

	// Dialing the slot rings the parked party (the parked dialog is not the
	// pickup dialog: the picker's From must show).
	picker.send(invite("pk-2", "300", "701", picker.addr, server), server)
	picker.expect("100 Trying", 2*time.Second)
	pickup := parker.expect("INVITE", 2*time.Second)
	if !strings.Contains(headerValue(pickup, "From"), "300") {
		t.Errorf("pickup INVITE not from the picker:\n%s", pickup)
	}

	// Answer connects both sides...
	parker.send(respond(pickup, 200, "OK", "100", parker), server)
	picker.expect("SIP/2.0 200", 2*time.Second)
	picker.send(ackOrBye("ACK", "pk-2", "300", "100", picker.addr, server, 1), server)

	// The parked dialog is released (BYE carries the parked Call-ID) while
	// the picker's ACK may arrive in either order — collect both.
	byeID, gotACK := "", false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (byeID == "" || !gotACK) {
		msg := parker.recv(time.Until(deadline))
		if msg == "" {
			break
		}
		switch {
		case strings.HasPrefix(msg, "BYE "):
			byeID = headerValue(msg, "Call-ID")
		case strings.HasPrefix(msg, "ACK "):
			gotACK = true
		}
	}
	if byeID != "pk-1" {
		t.Errorf("release BYE Call-ID = %q, want pk-1", byeID)
	}
	if !gotACK {
		t.Errorf("the picker's ACK never reached the parker")
	}

	// The slot is free again: a fresh *70 parks immediately.
	parker.send(invite("pk-3", "100", "*70", parker.addr, server), server)
	parker.expect("SIP/2.0 200", 2*time.Second)
}

func TestParkTimeoutReleasesSlot(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Park.TimeoutSeconds = 1
	server := startServer(t, cfg, dialplan.New())

	parker := newTestUA(t)
	parker.register("100", server)

	parker.send(invite("pk-t", "100", "*70", parker.addr, server), server)
	parker.expect("SIP/2.0 200", 2*time.Second)

	// Nobody picks it up: the server hangs the parked call up...
	bye := parker.expect("BYE", 3*time.Second)
	if v := headerValue(bye, "Call-ID"); v != "pk-t" {
		t.Errorf("timeout BYE Call-ID = %q, want pk-t", v)
	}
	parker.send(respond(bye, 200, "OK", "100", parker), server)

	// ... freeing the slot for the next parker.
	other := newTestUA(t)
	other.register("101", server)
	other.send(invite("pk-t2", "101", "*70", other.addr, server), server)
	other.expect("SIP/2.0 200", 2*time.Second)
}

func TestParkOrbitAllocationAndFull(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Park.OrbitSize = 2 // slots 701, 702
	server := startServer(t, cfg, dialplan.New())

	a := newTestUA(t)
	b := newTestUA(t)
	c := newTestUA(t)
	a.register("100", server)
	b.register("101", server)
	c.register("102", server)

	// Two parkers take the two slots (lowest free first).
	a.send(invite("orb-a", "100", "*70", a.addr, server), server)
	a.expect("SIP/2.0 200", 2*time.Second)
	b.send(invite("orb-b", "101", "*70", b.addr, server), server)
	b.expect("SIP/2.0 200", 2*time.Second)

	// The third park is rejected: the orbit is full.
	c.send(invite("orb-c", "102", "*70", c.addr, server), server)
	c.expect("503 All Park Slots Busy", 2*time.Second)

	// Slot 701 reaches the first parker, not the second.
	c.send(invite("orb-pick", "102", "701", c.addr, server), server)
	first := a.expect("INVITE", 2*time.Second)
	if !strings.Contains(headerValue(first, "From"), "102") {
		t.Errorf("pickup of 701 not from the third caller:\n%s", first)
	}
	b.expectNone(300 * time.Millisecond)
}

func TestParkedCallAbsorbsInDialogRefresh(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())

	parker := newTestUA(t)
	parker.register("100", server)

	parker.send(invite("pk-r", "100", "*70", parker.addr, server), server)
	resp := parker.expect("SIP/2.0 200", 2*time.Second)
	toTag := tagOf(headerValue(resp, "To"))
	if toTag == "" {
		t.Fatal("park answer has no To-tag")
	}

	// A session refresh from the parked party is answered locally; the
	// server must never forward it to the party itself.
	parker.send(dialogReq("INVITE", "pk-r", "100", "from-pk-r", "701", toTag, 2, parker, server, sdpFor("sendrecv")), server)
	parker.expect("SIP/2.0 200", 2*time.Second)
	parker.expectNone(300 * time.Millisecond)
}
