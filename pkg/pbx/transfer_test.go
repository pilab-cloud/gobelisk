package pbx

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
)

// ---- blind transfer (GOBELISK-11) ----

func TestBlindTransferReferNotifyAndNewCall(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())
	transferor := newTestUA(t) // A: 100, in the call, initiates the transfer
	transferee := newTestUA(t) // B: 201, gets REFER'd and places the new call
	target := newTestUA(t)     // C: 302, receives the transferred call
	transferor.register("100", server)
	transferee.register("201", server)
	target.register("302", server)
	establishCall(t, server, transferor, transferee, "xfer-1", "100", "201")

	// The transferor REFERs the transferee to sip:302.
	transferor.send(dialogReq("REFER", "xfer-1", "100", "from-xfer-1", "201", "callee", 2, transferor, server, "",
		map[string]string{"Refer-To": "<sip:302>", "Referred-By": "<sip:100>"}), server)
	ref := transferee.expect("REFER", 2*time.Second)
	if v := headerValue(ref, "Refer-To"); v != "<sip:302>" {
		t.Errorf("forwarded Refer-To = %q, want <sip:302>:\n%s", v, ref)
	}
	if v := headerValue(ref, "Referred-By"); v != "<sip:100>" {
		t.Errorf("forwarded Referred-By = %q, want <sip:100>", v)
	}

	// The transferee accepts the REFER; the 202 comes back to the transferor.
	transferee.send(respond(ref, 202, "Accepted", "201", transferee), server)
	resp := transferor.expect("SIP/2.0 202", 2*time.Second)
	if v := headerValue(resp, "CSeq"); v != "2 REFER" {
		t.Errorf("REFER answer CSeq = %q, want 2 REFER", v)
	}

	// NOTIFY: transfer progress travels back with its event headers and body...
	notify := dialogReq("NOTIFY", "xfer-1", "201", "callee", "100", "from-xfer-1", 3, transferee, server,
		"SIP/2.0 100 Trying\r\n",
		map[string]string{"Event": "refer", "Subscription-State": "active;expires=300", "Content-Type": "application/sipfrag"})
	transferee.send(notify, server)
	n := transferor.expect("NOTIFY", 2*time.Second)
	if v := headerValue(n, "Event"); v != "refer" {
		t.Errorf("forwarded NOTIFY Event = %q, want refer:\n%s", v, n)
	}
	if !strings.Contains(n, "application/sipfrag") {
		t.Errorf("forwarded NOTIFY lost Content-Type application/sipfrag:\n%s", n)
	}
	if !strings.Contains(n, "100 Trying") {
		t.Errorf("forwarded NOTIFY lost the sipfrag body:\n%s", n)
	}
	// ... and the transferor's 200 goes back to the transferee's transaction.
	transferor.send(respond(n, 200, "OK", "100", transferor), server)
	nresp := transferee.expect("SIP/2.0 200", 2*time.Second)
	if v := headerValue(nresp, "CSeq"); v != "3 NOTIFY" {
		t.Errorf("NOTIFY answer CSeq = %q, want 3 NOTIFY", v)
	}

	// The transferee places the new call: the transferred call connects.
	transferee.send(invite("xfer-2", "201", "302", transferee.addr, server), server)
	tinv := target.expect("INVITE", 2*time.Second)
	target.send(respond(tinv, 200, "OK", "302", target), server)
	transferee.expect("SIP/2.0 200", 2*time.Second)

	// And the original call is torn down (transferee BYEs the transferor).
	transferee.send(dialogReq("BYE", "xfer-1", "201", "callee", "100", "from-xfer-1", 4, transferee, server, ""), server)
	transferee.expect("SIP/2.0 200", 2*time.Second)
	transferor.expect("BYE", 2*time.Second)
}

// ---- attended transfer (GOBELISK-11) ----

func TestAttendedTransferReplaces(t *testing.T) {
	// Call waiting off: without Replaces handling, the takeover INVITE would
	// be rejected 486 by the busy consultation partner.
	cfg := config.Default()
	cfg.Features.CallWaiting = false
	server := startServer(t, cfg, dialplan.New())

	transferor := newTestUA(t) // A: 100
	transferee := newTestUA(t) // B: 201
	consulted := newTestUA(t)  // C: 401 — busy with the consultation call
	transferor.register("100", server)
	transferee.register("201", server)
	consulted.register("401", server)

	// A first talks to B (the call to transfer), then consults C.
	establishCall(t, server, transferor, transferee, "att-b", "100", "201")
	establishCall(t, server, transferor, consulted, "att-c", "100", "401")

	// A REFERs B with Refer-To carrying Replaces for the att-c dialog
	// (URI-parameter form, percent-encoded like real UAs send it).
	referTo := "<sip:401@x?Replaces=att-c%3Bto-tag%3Dcallee%3Bfrom-tag%3Dfrom-att-c>"
	transferor.send(dialogReq("REFER", "att-b", "100", "from-att-b", "201", "callee", 2, transferor, server, "",
		map[string]string{"Refer-To": referTo}), server)
	ref := transferee.expect("REFER", 2*time.Second)
	if v := headerValue(ref, "Refer-To"); v != referTo {
		t.Errorf("forwarded Refer-To = %q, want %q", v, referTo)
	}
	transferee.send(respond(ref, 202, "Accepted", "201", transferee), server)
	transferor.expect("SIP/2.0 202", 2*time.Second)

	// B completes the attended transfer: out-of-dialog INVITE to C carrying
	// Replaces, while C is still in the consultation call with A.
	transferee.send(dialogReq("INVITE", "att-new", "201", "from-att-new", "401", "", 1, transferee, server, sdpFor("sendrecv"),
		map[string]string{"Replaces": "att-c;to-tag=callee;from-tag=from-att-c"}), server)

	takeover := consulted.expect("INVITE", 2*time.Second)
	if v := headerValue(takeover, "Replaces"); v != "att-c;to-tag=callee;from-tag=from-att-c" {
		t.Errorf("Replaces header not forwarded: %q\n%s", v, takeover)
	}
	if a := headerValue(takeover, "Alert-Info"); a != "" {
		t.Errorf("Replaces takeover must not be treated as call waiting, got Alert-Info %q", a)
	}

	// The transferred call connects.
	consulted.send(respondSDP(takeover, 200, "OK", "401", consulted, sdpFor("sendrecv")), server)
	transferee.expect("SIP/2.0 200", 2*time.Second)
	transferee.send(dialogReq("ACK", "att-new", "201", "from-att-new", "401", "callee", 1, transferee, server, ""), server)
	consulted.expect("ACK", 2*time.Second)

	// C drops the replaced call with A.
	consulted.send(dialogReq("BYE", "att-c", "401", "callee", "100", "from-att-c", 2, consulted, server, ""), server)
	consulted.expect("SIP/2.0 200", 2*time.Second)
	transferor.expect("BYE", 2*time.Second)
}

// ---- Replaces URI parameter handling ----

func TestReplacesURIParamConvertedToHeader(t *testing.T) {
	server := startServer(t, config.Default(), dialplan.New())
	consulted := newTestUA(t) // C: 501
	consulted.register("501", server)

	// B's UA puts Replaces in the Request-URI query (percent-encoded).
	b := newTestUA(t)
	body := sdpFor("sendrecv")
	uri := fmt.Sprintf("sip:501@%s?Replaces=att-c%%3Bto-tag%%3Dcallee%%3Bfrom-tag%%3Dfrom-att-c", server)
	msg := fmt.Sprintf("INVITE %s SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP %s;branch=z9hG4bK-rep1\r\n"+
		"From: <sip:201@%s>;tag=from-rep1\r\n"+
		"To: <sip:501@%s>\r\n"+
		"Call-ID: rep-1\r\n"+
		"CSeq: 1 INVITE\r\n"+
		"Contact: <sip:201@%s>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Type: application/sdp\r\n"+
		"Content-Length: %d\r\n\r\n%s",
		uri, b.addr, b.addr, server, b.addr, len(body), body)

	b.send(msg, server)
	takeover := consulted.expect("INVITE", 2*time.Second)
	if v := headerValue(takeover, "Replaces"); v != "att-c;to-tag=callee;from-tag=from-att-c" {
		t.Errorf("Replaces URI param not converted to header, got %q:\n%s", v, takeover)
	}
}
