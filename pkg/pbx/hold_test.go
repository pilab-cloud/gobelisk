package pbx

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
)

// ---- SDP and in-dialog message builders ----

// sdpFor returns an audio SDP offer/answer with the given direction
// attribute at media level.
func sdpFor(dir string) string {
	return "v=0\r\n" +
		"o=- 1000 1000 IN IP4 127.0.0.1\r\n" +
		"s=-\r\n" +
		"c=IN IP4 127.0.0.1\r\n" +
		"t=0 0\r\n" +
		"m=audio 4000 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"a=" + dir + "\r\n"
}

// dialogReq builds an in-dialog request (re-INVITE, UPDATE, ACK, REFER,
// NOTIFY) sent by one party of an established dialog. body may be empty;
// extra carries additional headers (Refer-To, Event, Content-Type, ...).
func dialogReq(method, callID, fromExt, fromTag, toExt, toTag string, cseq int, ua *testUA, server *net.UDPAddr, body string, extra ...map[string]string) string {
	branch := fmt.Sprintf("z9hG4bK-%s-%d-%s", callID, cseq, method)
	to := fmt.Sprintf("<sip:%s@%s>", toExt, server)
	if toTag != "" {
		to += ";tag=" + toTag
	}
	msg := fmt.Sprintf("%s sip:%s@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP %s;branch=%s\r\n"+
		"From: <sip:%s@%s>;tag=%s\r\n"+
		"To: %s\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: %d %s\r\n"+
		"Contact: <sip:%s@%s>\r\n"+
		"Max-Forwards: 70\r\n",
		method, toExt, server,
		ua.addr, branch,
		fromExt, ua.addr, fromTag,
		to,
		callID, cseq, method,
		fromExt, ua.addr)
	hasCT := false
	for _, h := range extra {
		for k, v := range h {
			if strings.EqualFold(k, "Content-Type") {
				hasCT = true
			}
			msg += k + ": " + v + "\r\n"
		}
	}
	if body != "" && !hasCT {
		msg += "Content-Type: application/sdp\r\n"
	}
	return msg + fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

// respondSDP is respond() with an SDP answer body attached.
func respondSDP(req string, code int, reason, ext string, ua *testUA, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, reason)
	fmt.Fprintf(&b, "Via: %s\r\n", headerValue(req, "Via"))
	fmt.Fprintf(&b, "From: %s\r\n", headerValue(req, "From"))
	to := headerValue(req, "To")
	if !strings.Contains(to, "tag=") {
		to += ";tag=callee"
	}
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", headerValue(req, "Call-ID"))
	fmt.Fprintf(&b, "CSeq: %s\r\n", headerValue(req, "CSeq"))
	if code < 300 {
		fmt.Fprintf(&b, "Contact: <sip:%s@%s>\r\n", ext, ua.addr)
	}
	if body != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n", len(body))
	b.WriteString(body)
	return b.String()
}

// establishCall sets up and answers a call between caller and callee.
func establishCall(t *testing.T, server *net.UDPAddr, caller, callee *testUA, callID, callerExt, calleeExt string) {
	t.Helper()
	caller.send(invite(callID, callerExt, calleeExt, caller.addr, server), server)
	inv := callee.expect("INVITE", 2*time.Second)
	callee.send(respond(inv, 200, "OK", calleeExt, callee), server)
	caller.expect("SIP/2.0 200", 2*time.Second)
	caller.send(ackOrBye("ACK", callID, callerExt, calleeExt, caller.addr, server, 1), server)
	callee.expect("ACK", 2*time.Second)
}

// ---- hold / resume tests (GOBELISK-10) ----

func TestCallerInitiatedHoldAndResume(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())
	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("201", server)
	establishCall(t, server, caller, callee, "hold-1", "100", "201")

	// Hold: the caller's sendonly re-INVITE must reach the callee...
	caller.send(dialogReq("INVITE", "hold-1", "100", "from-hold-1", "201", "callee", 2, caller, server, sdpFor("sendonly")), server)
	inv := callee.expect("INVITE", 2*time.Second)
	if !strings.Contains(inv, "a=sendonly") {
		t.Errorf("callee re-INVITE lost the hold offer:\n%s", inv)
	}
	if v := headerValue(inv, "CSeq"); v != "2 INVITE" {
		t.Errorf("callee re-INVITE CSeq = %q, want 2 INVITE", v)
	}

	// ... and its answer must come back to the caller.
	callee.send(respondSDP(inv, 200, "OK", "201", callee, sdpFor("recvonly")), server)
	resp := caller.expect("SIP/2.0 200", 2*time.Second)
	if !strings.Contains(resp, "a=recvonly") {
		t.Errorf("hold answer lost the recvonly offer answer:\n%s", resp)
	}
	if c := headerValue(resp, "Contact"); !strings.Contains(c, "sip:server@") {
		t.Errorf("200 Contact = %q, want the server so dialogs stay anchored", c)
	}

	// The ACK completes the transaction at the callee.
	caller.send(ackOrBye("ACK", "hold-1", "100", "201", caller.addr, server, 2), server)
	callee.expect("ACK", 2*time.Second)

	// Resume: a sendrecv re-INVITE travels the same path back.
	caller.send(dialogReq("INVITE", "hold-1", "100", "from-hold-1", "201", "callee", 3, caller, server, sdpFor("sendrecv")), server)
	inv2 := callee.expect("INVITE", 2*time.Second)
	if !strings.Contains(inv2, "a=sendrecv") {
		t.Errorf("resume re-INVITE lost the sendrecv direction:\n%s", inv2)
	}
	callee.send(respondSDP(inv2, 200, "OK", "201", callee, sdpFor("sendrecv")), server)
	caller.expect("SIP/2.0 200", 2*time.Second)
	caller.send(ackOrBye("ACK", "hold-1", "100", "201", caller.addr, server, 3), server)
	callee.expect("ACK", 2*time.Second)
}

func TestCalleeInitiatedHoldAndResume(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())
	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("301", server)
	establishCall(t, server, caller, callee, "hold-2", "100", "301")

	// The callee places the caller on hold; it is the UAC of this re-INVITE.
	callee.send(dialogReq("INVITE", "hold-2", "301", "callee", "100", "from-hold-2", 2, callee, server, sdpFor("sendonly")), server)
	inv := caller.expect("INVITE", 2*time.Second)
	if !strings.Contains(inv, "a=sendonly") {
		t.Errorf("caller re-INVITE lost the hold offer:\n%s", inv)
	}

	// The caller answers; the response goes back to the callee.
	caller.send(respondSDP(inv, 200, "OK", "100", caller, sdpFor("recvonly")), server)
	callee.expect("SIP/2.0 200", 2*time.Second)

	// The callee's ACK must be routed to the caller, not back to itself.
	callee.send(dialogReq("ACK", "hold-2", "301", "callee", "100", "from-hold-2", 2, callee, server, ""), server)
	caller.expect("ACK", 2*time.Second)

	// Resume from the callee.
	callee.send(dialogReq("INVITE", "hold-2", "301", "callee", "100", "from-hold-2", 3, callee, server, sdpFor("sendrecv")), server)
	inv2 := caller.expect("INVITE", 2*time.Second)
	caller.send(respondSDP(inv2, 200, "OK", "100", caller, sdpFor("sendrecv")), server)
	callee.expect("SIP/2.0 200", 2*time.Second)
	callee.send(dialogReq("ACK", "hold-2", "301", "callee", "100", "from-hold-2", 3, callee, server, ""), server)
	caller.expect("ACK", 2*time.Second)
}

func TestHoldRejectedWhenDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Hold.Enabled = config.Bool(false)
	server := startServer(t, cfg, dialplan.New())
	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("401", server)
	establishCall(t, server, caller, callee, "hold-off", "100", "401")

	// A hold offer is rejected by policy...
	caller.send(dialogReq("INVITE", "hold-off", "100", "from-hold-off", "401", "callee", 2, caller, server, sdpFor("sendonly")), server)
	resp := caller.expect("SIP/2.0 403", 2*time.Second)
	if !strings.Contains(firstLine(resp), "403") {
		t.Errorf("want 403 Forbidden, got %q", firstLine(resp))
	}
	// ... and the far leg is never bothered.
	callee.expectNone(400 * time.Millisecond)

	// A plain session refresh (sendrecv) still goes through.
	caller.send(dialogReq("INVITE", "hold-off", "100", "from-hold-off", "401", "callee", 3, caller, server, sdpFor("sendrecv")), server)
	callee.expect("INVITE", 2*time.Second)
}

func TestHoldPerExtensionOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Hold.Enabled = config.Bool(false)
	cfg.Features.Hold.PerExtension = map[string]bool{"501": true}
	server := startServer(t, cfg, dialplan.New())
	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("501", server)
	establishCall(t, server, caller, callee, "hold-ov", "100", "501")

	// Caller 100 follows the global policy: rejected.
	caller.send(dialogReq("INVITE", "hold-ov", "100", "from-hold-ov", "501", "callee", 2, caller, server, sdpFor("sendonly")), server)
	caller.expect("SIP/2.0 403", 2*time.Second)
	callee.expectNone(300 * time.Millisecond)

	// Callee 501 is overridden on: its hold is forwarded.
	callee.send(dialogReq("INVITE", "hold-ov", "501", "callee", "100", "from-hold-ov", 2, callee, server, sdpFor("sendonly")), server)
	inv := caller.expect("INVITE", 2*time.Second)
	if !strings.Contains(inv, "a=sendonly") {
		t.Errorf("override hold not forwarded:\n%s", inv)
	}
	caller.send(respondSDP(inv, 200, "OK", "100", caller, sdpFor("recvonly")), server)
	callee.expect("SIP/2.0 200", 2*time.Second)
}

func TestHoldViaUpdateForwarded(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())
	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("601", server)
	establishCall(t, server, caller, callee, "hold-upd", "100", "601")

	caller.send(dialogReq("UPDATE", "hold-upd", "100", "from-hold-upd", "601", "callee", 2, caller, server, sdpFor("sendonly")), server)
	upd := callee.expect("UPDATE", 2*time.Second)
	if !strings.Contains(upd, "a=sendonly") {
		t.Errorf("UPDATE hold not forwarded:\n%s", upd)
	}
	callee.send(respondSDP(upd, 200, "OK", "601", callee, sdpFor("recvonly")), server)
	resp := caller.expect("SIP/2.0 200", 2*time.Second)
	if v := headerValue(resp, "CSeq"); v != "2 UPDATE" {
		t.Errorf("UPDATE answer CSeq = %q, want 2 UPDATE", v)
	}
}

func TestInDialogRequestFromStrangerRejected(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())
	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("701", server)
	establishCall(t, server, caller, callee, "hold-str", "100", "701")

	stranger := newTestUA(t)
	stranger.send(dialogReq("INVITE", "hold-str", "666", "stranger-tag", "701", "callee", 2, stranger, server, sdpFor("sendonly")), server)
	resp := stranger.expect("SIP/2.0 403", 2*time.Second)
	if !strings.Contains(firstLine(resp), "403") {
		t.Errorf("want 403 for non-party, got %q", firstLine(resp))
	}
	callee.expectNone(300 * time.Millisecond)
	caller.expectNone(300 * time.Millisecond)
}

func TestRetransmittedReinviteForwardedOnce(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())
	caller, callee := newTestUA(t), newTestUA(t)
	callee.register("801", server)
	establishCall(t, server, caller, callee, "hold-ret", "100", "801")

	msg := dialogReq("INVITE", "hold-ret", "100", "from-hold-ret", "801", "callee", 2, caller, server, sdpFor("sendonly"))
	caller.send(msg, server)
	// Retransmit after the first forward is registered (T1-scale retry).
	time.Sleep(150 * time.Millisecond)
	caller.send(msg, server)

	callee.expect("INVITE", 2*time.Second)
	// The duplicate must not be forwarded a second time.
	callee.expectNone(500 * time.Millisecond)
}

// ---- direction parsing and hold state units ----

func TestMediaDirectionParsing(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no sdp", "", ""},
		{"no explicit direction", "v=0\r\nm=audio 4000 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n", ""},
		{"media level", "v=0\r\nm=audio 4000 RTP/AVP 0\r\na=sendonly\r\n", "sendonly"},
		{"session level", "v=0\r\na=inactive\r\nm=audio 4000 RTP/AVP 0\r\n", "inactive"},
		{"media overrides session", "v=0\r\na=sendonly\r\nm=audio 4000 RTP/AVP 0\r\na=sendrecv\r\n", "sendrecv"},
		{"second stream ignored", "v=0\r\nm=audio 1 RTP/AVP 0\r\na=sendonly\r\nm=video 2 RTP/AVP 96\r\na=recvonly\r\n", "sendonly"},
	}
	for _, c := range cases {
		if got := mediaDirection(c.body); got != c.want {
			t.Errorf("%s: mediaDirection = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHoldStateTransitions(t *testing.T) {
	c := &Call{}
	if got := c.HoldState(); got != "active" {
		t.Fatalf("initial HoldState = %q, want active", got)
	}

	// An unanswered offer must not change the state yet.
	c.setHoldOffer("caller", "sendonly")
	if got := c.HoldState(); got != "active" {
		t.Errorf("pending offer changed state to %q", got)
	}
	c.applyHoldAnswer(true)
	if got := c.HoldState(); got != "held-by-caller" {
		t.Errorf("after accepted hold: %q, want held-by-caller", got)
	}

	// Resume clears it.
	c.setHoldOffer("caller", "sendrecv")
	c.applyHoldAnswer(true)
	if got := c.HoldState(); got != "active" {
		t.Errorf("after resume: %q, want active", got)
	}

	// A rejected hold leaves the call untouched.
	c.setHoldOffer("callee", "inactive")
	c.applyHoldAnswer(false)
	if got := c.HoldState(); got != "active" {
		t.Errorf("after rejected hold: %q, want active", got)
	}
}
