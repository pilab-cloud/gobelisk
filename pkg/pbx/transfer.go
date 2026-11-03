package pbx

import (
	"net/url"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip"
)

// handleRefer forwards a transfer request to the far leg (RFC 5589): the
// transferor REFERs the transferee, which then places the new call and
// reports back with NOTIFY (GOBELISK-11).
func (s *Server) handleRefer(m sipMsg) {
	req := m.msg
	call := s.getCall(getHeader(req, "Call-ID"))
	if call == nil {
		if inDialog(req) {
			m.respond(buildResponse(req, 481, "Call/Transaction Does Not Exist", nil, ""))
		} else {
			m.respond(buildResponse(req, 400, "Bad Request", nil, ""))
		}
		return
	}
	s.log.Info().Str("callID", call.CallID).Str("referTo", getHeader(req, "Refer-To")).Msg("REFER")
	s.forwardInDialog(m, call, "REFER")
}

// handleNotify relays transfer progress notifications between the legs
// (GOBELISK-11).
func (s *Server) handleNotify(m sipMsg) {
	req := m.msg
	call := s.getCall(getHeader(req, "Call-ID"))
	if call == nil {
		if inDialog(req) {
			m.respond(buildResponse(req, 481, "Call/Transaction Does Not Exist", nil, ""))
		} else {
			m.respond(buildResponse(req, 200, "OK", nil, ""))
		}
		return
	}
	s.forwardInDialog(m, call, "NOTIFY")
}

// replacesParam extracts a Replaces URI parameter from a Request-URI
// ("sip:401@host?Replaces=callid;to-tag=..;from-tag=.."), unescaped. Phones
// may carry Replaces either as a header or this way (RFC 3891 §3); a proxy
// must convert the parameter into a header for the next hop (GOBELISK-11).
func replacesParam(target string) string {
	i := strings.Index(target, "?")
	if i < 0 {
		return ""
	}
	for _, kv := range strings.Split(target[i+1:], "&") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Replaces") {
			v, err := url.QueryUnescape(parts[1])
			if err != nil {
				return parts[1]
			}
			return v
		}
	}
	return ""
}

// hasReplaces reports whether a request carries a Replaces (header or URI
// parameter), i.e. it is an attended-transfer takeover of an existing dialog.
func hasReplaces(req *sip.RawMessage) bool {
	return getHeader(req, "Replaces") != "" || replacesParam(req.Target) != ""
}
