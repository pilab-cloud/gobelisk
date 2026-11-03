package pbx

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
	"github.com/devopshaven/gobelisk/pkg/ivr"
	"github.com/rs/zerolog"
)

// ---- test SIP user agent ----

type testUA struct {
	t    *testing.T
	conn *net.UDPConn
	addr *net.UDPAddr
}

func newTestUA(t *testing.T) *testUA {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testUA{t: t, conn: conn, addr: conn.LocalAddr().(*net.UDPAddr)}
}

func (u *testUA) send(msg string, to *net.UDPAddr) {
	u.t.Helper()
	if _, err := u.conn.WriteToUDP([]byte(msg), to); err != nil {
		u.t.Fatalf("send: %v", err)
	}
}

// recv reads one datagram, returning "" on timeout.
func (u *testUA) recv(timeout time.Duration) string {
	u.t.Helper()
	buf := make([]byte, 65535)
	u.conn.SetReadDeadline(time.Now().Add(timeout))
	n, _, err := u.conn.ReadFromUDP(buf)
	if err != nil {
		return ""
	}
	return string(buf[:n])
}

func (u *testUA) expect(substr string, timeout time.Duration) string {
	u.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg := u.recv(time.Until(deadline))
		if msg == "" {
			break
		}
		if strings.Contains(msg, substr) {
			return msg
		}
	}
	u.t.Fatalf("%s: expected %q, got nothing", u.addr, substr)
	return ""
}

func (u *testUA) expectNone(timeout time.Duration) {
	u.t.Helper()
	if msg := u.recv(timeout); msg != "" {
		u.t.Fatalf("%s: expected no message, got %q", u.addr, firstLine(msg))
	}
}

func (u *testUA) register(ext string, server *net.UDPAddr) {
	u.t.Helper()
	msg := fmt.Sprintf("REGISTER sip:%s SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP %s;branch=z9hG4bKreg-%s\r\n"+
		"From: <sip:%s@%s>;tag=reg-%s\r\n"+
		"To: <sip:%s@%s>\r\n"+
		"Call-ID: reg-%s\r\n"+
		"CSeq: 1 REGISTER\r\n"+
		"Contact: <sip:%s@%s>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n\r\n",
		ext, u.addr, ext, ext, u.addr, ext, ext, u.addr, ext, ext, u.addr)
	u.send(msg, server)
	u.expect("SIP/2.0 200", 2*time.Second)
}

func invite(callID, fromExt, toExt string, caller *net.UDPAddr, server *net.UDPAddr) string {
	return fmt.Sprintf("INVITE sip:%s@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP %s;branch=z9hG4bK-%s\r\n"+
		"From: <sip:%s@%s>;tag=from-%s\r\n"+
		"To: <sip:%s@%s>\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: 1 INVITE\r\n"+
		"Contact: <sip:%s@%s>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n\r\n",
		toExt, server, caller, callID, fromExt, caller, callID, toExt, server, callID, fromExt, caller)
}

func ackOrBye(method, callID, fromExt, toExt string, caller *net.UDPAddr, server *net.UDPAddr, cseq int) string {
	return fmt.Sprintf("%s sip:%s@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP %s;branch=z9hG4bK-%s-%d\r\n"+
		"From: <sip:%s@%s>;tag=from-%s\r\n"+
		"To: <sip:%s@%s>;tag=callee\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: %d %s\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n\r\n",
		method, toExt, server, caller, callID, cseq, fromExt, caller, callID, toExt, server, callID, cseq, method)
}

// respond builds a response to a request, echoing the dialog headers.
func respond(req string, code int, reason, ext string, ua *testUA) string {
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
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

func headerValue(msg, name string) string {
	for _, line := range strings.Split(msg, "\r\n") {
		if i := strings.Index(line, ":"); i > 0 && strings.EqualFold(strings.TrimSpace(line[:i]), name) {
			return strings.TrimSpace(line[i+1:])
		}
	}
	return ""
}

func firstLine(msg string) string {
	if i := strings.Index(msg, "\r\n"); i >= 0 {
		return msg[:i]
	}
	return msg
}

// ---- server harness ----

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

func startServer(t *testing.T, cfg *config.Config, dp *dialplan.DialPlan) *net.UDPAddr {
	t.Helper()
	port := freeUDPPort(t)
	s := NewServer(
		WithConfig(cfg),
		WithDialPlan(dp),
		WithLogger(zerolog.Nop()),
	)
	ready := make(chan struct{})
	s.onListen = func() { close(ready) }
	go func() { _ = s.ListenUDP(port) }()
	t.Cleanup(func() { s.Close() })

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start listening")
	}

	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
}

func ringGroupDialplan(t *testing.T, line string) *dialplan.DialPlan {
	t.Helper()
	dp := dialplan.New()
	if err := dialplan.ApplyConfig(dp, ivr.NewManager(), line); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	return dp
}

// ---- ring group tests ----

func TestRingGroupSimultaneousForksAndBridges(t *testing.T) {
	cfg := config.Default()
	cfg.Features.RingTimeoutSeconds = 3

	server := startServer(t, cfg, ringGroupDialplan(t, "ringgroup 600 -> 101 102\n"))

	ua101 := newTestUA(t)
	ua102 := newTestUA(t)
	ua101.register("101", server)
	ua102.register("102", server)

	caller := newTestUA(t)
	callID := "ring-sim"
	caller.send(invite(callID, "100", "600", caller.addr, server), server)

	caller.expect("100 Trying", 2*time.Second)

	// Both members must be INVITEd simultaneously.
	inv101 := ua101.expect("INVITE", 2*time.Second)
	ua102.expect("INVITE", 2*time.Second)

	// 101 answers first.
	ua101.send(respond(inv101, 200, "OK", "101", ua101), server)
	caller.expect("SIP/2.0 200", 2*time.Second)

	// The loser is cancelled.
	ua102.expect("CANCEL", 2*time.Second)

	// ACK and BYE are routed to the winner.
	caller.send(ackOrBye("ACK", callID, "100", "101", caller.addr, server, 1), server)
	ua101.expect("ACK", 2*time.Second)

	caller.send(ackOrBye("BYE", callID, "100", "101", caller.addr, server, 2), server)
	caller.expect("SIP/2.0 200", 2*time.Second)
	ua101.expect("BYE", 2*time.Second)
}

func TestRingGroupSkipsUnavailableMembers(t *testing.T) {
	cfg := config.Default()
	cfg.Features.RingTimeoutSeconds = 2

	// 101 is in the group but never registers; only 102 can be rung.
	server := startServer(t, cfg, ringGroupDialplan(t, "ringgroup 600 -> 101 102\n"))

	ua102 := newTestUA(t)
	ua102.register("102", server)

	caller := newTestUA(t)
	caller.send(invite("ring-skip", "100", "600", caller.addr, server), server)
	caller.expect("100 Trying", 2*time.Second)

	inv := ua102.expect("INVITE", 2*time.Second)
	ua102.send(respond(inv, 486, "Busy Here", "102", ua102), server)

	caller.expect("480 Temporarily Unavailable", 3*time.Second)
}

func TestRingGroupRoundRobinPicksNextMember(t *testing.T) {
	cfg := config.Default()
	cfg.Features.RingTimeoutSeconds = 3

	server := startServer(t, cfg, ringGroupDialplan(t, "ringgroup 600 strategy round-robin -> 101 102\n"))

	ua101 := newTestUA(t)
	ua102 := newTestUA(t)
	ua101.register("101", server)
	ua102.register("102", server)

	// First call: 101 is rung first and answers.
	caller := newTestUA(t)
	caller.send(invite("rr-1", "100", "600", caller.addr, server), server)
	caller.expect("100 Trying", 2*time.Second)
	inv := ua101.expect("INVITE", 2*time.Second)
	ua102.expectNone(200 * time.Millisecond)
	ua101.send(respond(inv, 200, "OK", "101", ua101), server)
	caller.expect("SIP/2.0 200", 2*time.Second)
	caller.send(ackOrBye("ACK", "rr-1", "100", "101", caller.addr, server, 1), server)
	ua101.expect("ACK", 2*time.Second)
	// End the first call so 101 is free again.
	caller.send(ackOrBye("BYE", "rr-1", "100", "101", caller.addr, server, 2), server)
	caller.expect("SIP/2.0 200", 2*time.Second)
	ua101.expect("BYE", 2*time.Second)

	// Second call: round-robin starts with 102.
	caller2 := newTestUA(t)
	caller2.send(invite("rr-2", "100", "600", caller2.addr, server), server)
	caller2.expect("100 Trying", 2*time.Second)
	inv2 := ua102.expect("INVITE", 2*time.Second)
	ua101.expectNone(200 * time.Millisecond)
	ua102.send(respond(inv2, 486, "Busy Here", "102", ua102), server)
	// 102 rejected, so 101 must be tried next.
	inv101b := ua101.expect("INVITE", 2*time.Second)
	ua101.send(respond(inv101b, 200, "OK", "101", ua101), server)
	caller2.expect("SIP/2.0 200", 2*time.Second)
}

// ---- call waiting tests ----

func TestCallWaitingForwardsSecondCallToBusyExtension(t *testing.T) {
	cfg := config.Default() // call waiting enabled by default
	server := startServer(t, cfg, dialplan.New())

	callee := newTestUA(t)
	callee.register("201", server)

	// First call is established.
	caller1 := newTestUA(t)
	caller1.send(invite("cw-1", "100", "201", caller1.addr, server), server)
	inv1 := callee.expect("INVITE", 2*time.Second)
	callee.send(respond(inv1, 200, "OK", "201", callee), server)
	caller1.expect("SIP/2.0 200", 2*time.Second)
	caller1.send(ackOrBye("ACK", "cw-1", "100", "201", caller1.addr, server, 1), server)
	callee.expect("ACK", 2*time.Second)

	// Second call must reach the busy extension instead of getting 486.
	caller2 := newTestUA(t)
	caller2.send(invite("cw-2", "101", "201", caller2.addr, server), server)
	caller2.expect("100 Trying", 2*time.Second)

	inv2 := callee.expect("INVITE", 2*time.Second)
	if headerValue(inv2, "Alert-Info") == "" {
		t.Errorf("call-waiting INVITE has no Alert-Info header:\n%s", inv2)
	}
	callee.send(respond(inv2, 486, "Busy Here", "201", callee), server)
	caller2.expect("486 Busy Here", 2*time.Second)
}

func TestCallWaitingDisabledRejectsBusyExtension(t *testing.T) {
	cfg := config.Default()
	cfg.Features.CallWaiting = false
	server := startServer(t, cfg, dialplan.New())

	callee := newTestUA(t)
	callee.register("301", server)

	caller1 := newTestUA(t)
	caller1.send(invite("cwd-1", "100", "301", caller1.addr, server), server)
	inv1 := callee.expect("INVITE", 2*time.Second)
	callee.send(respond(inv1, 200, "OK", "301", callee), server)
	caller1.expect("SIP/2.0 200", 2*time.Second)
	caller1.send(ackOrBye("ACK", "cwd-1", "100", "301", caller1.addr, server, 1), server)
	callee.expect("ACK", 2*time.Second)

	caller2 := newTestUA(t)
	caller2.send(invite("cwd-2", "101", "301", caller2.addr, server), server)
	caller2.expect("486 Busy Here", 2*time.Second)

	// The busy phone must not be disturbed.
	callee.expectNone(300 * time.Millisecond)
}

func TestCallWaitingPerExtensionOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Features.CallWaiting = false
	cfg.Features.CallWaitingExtensions = map[string]bool{"401": true}
	server := startServer(t, cfg, dialplan.New())

	ua401 := newTestUA(t)
	ua401.register("401", server)

	// 401 is overridden on, so its second call is delivered.
	caller1 := newTestUA(t)
	caller1.send(invite("ov-1", "100", "401", caller1.addr, server), server)
	inv1 := ua401.expect("INVITE", 2*time.Second)
	ua401.send(respond(inv1, 200, "OK", "401", ua401), server)
	caller1.expect("SIP/2.0 200", 2*time.Second)
	caller1.send(ackOrBye("ACK", "ov-1", "100", "401", caller1.addr, server, 1), server)
	ua401.expect("ACK", 2*time.Second)

	caller2 := newTestUA(t)
	caller2.send(invite("ov-2", "101", "401", caller2.addr, server), server)
	caller2.expect("100 Trying", 2*time.Second)
	ua401.expect("INVITE", 2*time.Second)
}
