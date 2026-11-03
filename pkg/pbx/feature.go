package pbx

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/devopshaven/gobelisk/pkg/sip"
)

// featureEntry caches the answer sent for a feature-code INVITE so a
// retransmission gets the same response instead of executing twice.
type featureEntry struct {
	resp string
	at   time.Time
}

// featureTTL keeps those answers around long enough to cover SIP
// retransmissions (64*T1 = 32s) with margin (GOBELISK-18).
const featureTTL = 60 * time.Second

// seedDND (re)loads the runtime DND set from configuration. Feature codes
// change the set until the next reload (GOBELISK-14 / GOBELISK-18).
func (s *Server) seedDND() {
	s.dndMu.Lock()
	defer s.dndMu.Unlock()
	s.dndOn = make(map[string]bool)
	if s.cfg == nil {
		return
	}
	for _, ext := range s.cfg.Features.DNDExtensions {
		s.dndOn[ext] = true
	}
}

// setDND switches Do Not Disturb for an extension at runtime.
func (s *Server) setDND(ext string, on bool) {
	s.dndMu.Lock()
	defer s.dndMu.Unlock()
	if on {
		s.dndOn[ext] = true
	} else {
		delete(s.dndOn, ext)
	}
}

// featureCode intercepts a dialed feature code (GOBELISK-18). It returns
// intercepted=false when dialed is not a code. For an executed code the
// response is already sent and routeTo is empty; for park the action is "go
// to the next free orbit slot" and routeTo carries that slot's number, so
// routing continues in handleInvite.
func (s *Server) featureCode(m sipMsg, dialed string) (intercepted bool, routeTo string) {
	req := m.msg
	if s.cfg == nil || !s.cfg.FeatureCodesEnabled() || dialed == "" {
		return false, ""
	}
	fc := s.cfg.Features.FeatureCodes
	action := ""
	switch dialed {
	case fc.DNDOn:
		action = "dnd_on"
	case fc.DNDOff:
		action = "dnd_off"
	case fc.Park:
		action = "park"
	default:
		return false, ""
	}

	// Only a registered extension may operate its own features.
	callerExt := userFromURI(getHeader(req, "From"))
	if callerExt == "" || s.getReg(callerExt) == nil {
		s.log.Warn().Str("code", dialed).Str("caller", callerExt).Msg("feature code from unregistered caller")
		m.respond(buildResponse(req, 403, "Forbidden", nil, ""))
		return true, ""
	}

	// A retransmission replays the cached answer instead of re-executing.
	cacheKey := getHeader(req, "Call-ID") + "|" + cseqNumber(req)
	if resp, ok := s.cachedFeature(cacheKey); ok {
		m.respond(resp)
		return true, ""
	}

	switch action {
	case "dnd_on", "dnd_off":
		s.setDND(callerExt, action == "dnd_on")
		s.log.Info().Str("ext", callerExt).Str("code", dialed).Bool("dnd", action == "dnd_on").Msg("feature code: DND")
		s.cacheFeature(cacheKey, s.answerFeature(m))
		return true, ""
	case "park":
		slot, ok := s.nextFreePark()
		if !ok {
			s.log.Warn().Str("caller", callerExt).Msg("park orbit full")
			m.respond(buildResponse(req, 503, "All Park Slots Busy", nil, ""))
			return true, ""
		}
		s.log.Info().Str("caller", callerExt).Int("slot", slot).Msg("feature code: park")
		return true, s.cfg.ParkNumber(slot)
	}
	return false, ""
}

// answerFeature replies 200 to a feature-code INVITE — with a To-tag so the
// dialog forms — and hangs the call up shortly after, because the PBX has no
// media path to play confirmation tones (GOBELISK-18). The returned response
// is cached for retransmissions.
func (s *Server) answerFeature(m sipMsg) string {
	req := m.msg
	tag := fmt.Sprintf("fc%x", time.Now().UnixNano())
	extra := map[string]string{
		"To":      withTag(getHeader(req, "To"), tag),
		"Contact": s.serverContact(),
	}
	body := ""
	if req.Body != "" {
		body = s.inactiveSDP()
	}
	resp := buildResponse(req, 200, "OK", extra, body)
	m.respond(resp)

	// Give the phone a moment to ACK, then end the dialog.
	orig, ourTag, addr := req, tag, m.addr
	time.AfterFunc(1500*time.Millisecond, func() {
		if bye := s.dialogBYE(orig, ourTag, addr); bye != "" {
			s.sendRaw(addr, bye)
		}
	})
	return resp
}

// dialogBYE builds a BYE this server sends as the UAS of a dialog it
// answered itself (feature codes), carrying our to-tag and a CSeq above the
// caller's INVITE number as RFC 3261 §12.2 requires (GOBELISK-18).
func (s *Server) dialogBYE(invite *sip.RawMessage, ourTag string, addr *net.UDPAddr) string {
	target := strings.Trim(getHeader(invite, "Contact"), "<>")
	if target == "" {
		ext := userFromURI(getHeader(invite, "From"))
		if ext == "" || addr == nil {
			return ""
		}
		target = fmt.Sprintf("sip:%s@%s", ext, addr.String())
	}
	return s.buildBYE(target, ourTag, getHeader(invite, "From"),
		getHeader(invite, "Call-ID"), strconv.Itoa(cseqNum(invite)+1))
}

// buildBYE assembles a server-initiated BYE within an existing dialog.
func (s *Server) buildBYE(target, fromTag, toHeader, callID, cseq string) string {
	return fmt.Sprintf("BYE %s SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
		"From: <sip:server@%s:%d>;tag=%s\r\n"+
		"To: %s\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: %s BYE\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n\r\n",
		target, s.host, s.port, newBranch(), s.host, s.port, fromTag,
		toHeader, callID, cseq)
}

// inactiveSDP answers an offer with a stream nobody sends: feature and
// parked calls are signaling-only, the PBX anchors no media (GOBELISK-12/18).
func (s *Server) inactiveSDP() string {
	return "v=0\r\n" +
		"o=- 0 0 IN IP4 " + s.host + "\r\n" +
		"s=-\r\n" +
		"c=IN IP4 " + s.host + "\r\n" +
		"t=0 0\r\n" +
		"m=audio 9 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"a=inactive\r\n"
}

// withTag appends a tag to a From/To header value that has none.
func withTag(header, tag string) string {
	if header == "" || tagOf(header) != "" {
		return header
	}
	return header + ";tag=" + tag
}

// cseqNum returns the numeric part of a CSeq header (0 when unparsable).
func cseqNum(req *sip.RawMessage) int {
	n, _ := strconv.Atoi(cseqNumber(req))
	return n
}

// cacheFeature stores the answer for a feature-code transaction.
func (s *Server) cacheFeature(key, resp string) {
	s.featureMu.Lock()
	defer s.featureMu.Unlock()
	now := time.Now()
	for k, e := range s.featureLast {
		if now.Sub(e.at) > featureTTL {
			delete(s.featureLast, k)
		}
	}
	s.featureLast[key] = featureEntry{resp: resp, at: now}
}

// cachedFeature returns the cached answer of a feature-code transaction.
func (s *Server) cachedFeature(key string) (string, bool) {
	s.featureMu.Lock()
	defer s.featureMu.Unlock()
	e, ok := s.featureLast[key]
	if !ok || time.Since(e.at) > featureTTL {
		return "", false
	}
	return e.resp, true
}
