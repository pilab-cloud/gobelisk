package pbx

import (
	"fmt"
	"net"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip"
)

// tagOf returns the tag parameter of a From/To header value, or "".
func tagOf(header string) string {
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if len(part) > 4 && strings.EqualFold(part[:4], "tag=") {
			return part[4:]
		}
	}
	return ""
}

// cseqNumber returns the number part of a CSeq header ("5 INVITE" -> "5").
func cseqNumber(req *sip.RawMessage) string {
	if parts := strings.Fields(getHeader(req, "CSeq")); len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// inDialog reports whether a request carries a To-tag and therefore belongs
// to an established (or early) dialog.
func inDialog(req *sip.RawMessage) bool {
	return tagOf(getHeader(req, "To")) != ""
}

// legOf identifies which side of the dialog sent a message: "caller",
// "callee" or "" when the sender is not a dialog party. The dialog tags are
// authoritative; the source address is the fallback (GOBELISK-10).
func (c *Call) legOf(m sipMsg) string {
	if m.msg == nil {
		return ""
	}
	fromTag := tagOf(getHeader(m.msg, "From"))
	if c.CallerTag != "" && fromTag == c.CallerTag {
		return "caller"
	}
	if c.CalleeTag != "" && fromTag == c.CalleeTag {
		return "callee"
	}
	// Fallback: source address comparison (no/partial tags).
	if m.addr != nil && c.CallerAddr != nil && m.addr.String() == c.CallerAddr.String() {
		return "caller"
	}
	if m.addr != nil && c.CalleeAddr != nil && m.addr.String() == c.CalleeAddr.String() {
		return "callee"
	}
	return ""
}

// otherAddr returns the address of the opposite leg.
func (c *Call) otherAddr(leg string) *net.UDPAddr {
	if leg == "caller" {
		return c.CalleeAddr
	}
	return c.CallerAddr
}

// otherLeg returns the opposite leg name.
func otherLeg(leg string) string {
	if leg == "caller" {
		return "callee"
	}
	return "caller"
}

// extOf returns the extension of a leg.
func (c *Call) extOf(leg string) string {
	if leg == "caller" {
		return c.CallerExt
	}
	return c.CalleeExt
}

// remoteTarget returns the Request-URI to reach a leg: the Contact the leg
// supplied when it joined the dialog, falling back to user@address.
func (c *Call) remoteTarget(leg string) string {
	if leg == "callee" {
		if c.CalleeContact != "" {
			return strings.Trim(c.CalleeContact, "<>")
		}
		if c.CalleeAddr != nil {
			return fmt.Sprintf("sip:%s@%s", c.CalleeExt, c.CalleeAddr.String())
		}
		return ""
	}
	if c.CallerContact != "" {
		return strings.Trim(c.CallerContact, "<>")
	}
	if c.CallerAddr != nil {
		return fmt.Sprintf("sip:%s@%s", c.CallerExt, c.CallerAddr.String())
	}
	return ""
}

// setCalleeIdentity records the callee's dialog tag and Contact once it
// answered (GOBELISK-10).
func (c *Call) setCalleeIdentity(toHeader, contact string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tag := tagOf(toHeader); tag != "" {
		c.CalleeTag = tag
	}
	if contact != "" {
		c.CalleeContact = contact
	}
}
