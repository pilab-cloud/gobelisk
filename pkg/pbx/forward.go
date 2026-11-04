package pbx

import (
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
	"github.com/devopshaven/gobelisk/pkg/sip"
)

// maxForwardHops caps how often one call may be redirected by call
// forwarding, so misconfigured chains terminate with 508 Loop Detected
// (GOBELISK-15).
const maxForwardHops = 5

// dialResult is the outcome of routing a dialed number: either a ring-group
// fork target or a registered extension that should ring.
type dialResult struct {
	// ringGroup, when set, means: fork to this group instead of one callee.
	ringGroup   string
	target      dialplan.Target
	calleeExt   string
	reg         *Registration
	callWaiting bool
	fwd         config.ForwardConfig
	// parkSlot is the orbit slot a park number dialed. parked marks the dial
	// as a park-slot dial at all; parkEmpty means the caller waits at the
	// server, otherwise it is the pickup of a parked call (GOBELISK-12).
	parked    bool
	parkSlot  int
	parkEmpty bool
	// trunk is set when the call goes out on an outbound route: dialNumber
	// is the number to dial after prefix stripping (GOBELISK-21/22).
	trunk      *trunkState
	dialNumber string
}

// dndEnabled reports whether ext has Do Not Disturb on. The runtime set is
// seeded from configuration and toggled by feature codes (GOBELISK-14 / 18).
func (s *Server) dndEnabled(ext string) bool {
	s.dndMu.Lock()
	defer s.dndMu.Unlock()
	return s.dndOn[ext]
}

// forwardConfig returns the call-forwarding rules of ext (GOBELISK-15).
func (s *Server) forwardConfig(ext string) config.ForwardConfig {
	if s.cfg == nil {
		return config.ForwardConfig{}
	}
	return s.cfg.Forward(ext)
}

// outboundDial picks the trunk for a dialed number (GOBELISK-21 /
// GOBELISK-22): every matching route whose trunk is dialable, ranked by
// cost — lower wins, ties keep the config order. Returns the trunk, the
// number after prefix stripping and the route name.
func (s *Server) outboundDial(dialed string) (*trunkState, string, string, bool) {
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()

	type candidate struct {
		route config.OutboundRoute
		st    *trunkState
	}
	var candidates []candidate
	for _, r := range s.cfg.OutboundRoutes {
		if !r.Match(dialed) {
			continue
		}
		st, ok := s.trunkStates[r.Trunk]
		if !ok || !st.dialable() {
			s.log.Debug().Str("route", r.Name).Str("trunk", r.Trunk).Msg("outbound route skipped: trunk unavailable")
			continue
		}
		candidates = append(candidates, candidate{route: r, st: st})
	}
	if len(candidates) == 0 {
		return nil, "", "", false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].route.Cost < candidates[j].route.Cost
	})

	best := candidates[0]
	number := dialed
	if n := best.route.PrefixStrip; n > 0 {
		if n >= len(number) {
			s.log.Warn().Str("route", best.route.Name).Str("dialed", dialed).Msg("prefix strip removes the whole number")
			return nil, "", "", false
		}
		number = number[n:]
	}
	return best.st, number, best.route.Name, true
}

// resolveCallee routes a dialed number through the dialplan and the
// DND / call-forwarding rules until the call has a ringing destination.
// It sends its own error responses (603 DND, 404 unregistered, 486 busy,
// 508 forwarding loop), so ok=false means the caller must return; a non-nil
// result must be dispatched by the caller (ring group vs single callee).
func (s *Server) resolveCallee(m sipMsg, dialed string, hops *int) (*dialResult, bool) {
	req := m.msg
	for *hops < maxForwardHops {
		target := s.dialPlan.Parse(dialed)
		s.log.Debug().Str("dialed", dialed).Str("targetType", string(target.Type)).Str("targetValue", target.Value).Msg("dialplan resolved")

		if target.Type == dialplan.TargetRingGroup {
			return &dialResult{ringGroup: target.Value, target: target}, true
		}

		calleeExt, ok := s.dialPlan.ResolveExtension(target)
		if !ok {
			s.log.Debug().Str("dialed", dialed).Msg("no explicit dialplan route, treating as extension")
			calleeExt = dialed
		}

		// DND wins over forwarding and ringing: the extension declines.
		if s.dndEnabled(calleeExt) {
			s.log.Info().Str("callee", calleeExt).Msg("callee has DND enabled")
			m.respond(buildResponse(req, 603, "Declined", nil, ""))
			return nil, false
		}

		fwd := s.forwardConfig(calleeExt)

		// Unconditional forwarding (CFU) rings the target instead —
		// registered or not, that is the point of CFU.
		if fwd.Unconditional != "" {
			s.log.Info().Str("callee", calleeExt).Str("to", fwd.Unconditional).Msg("call forwarded unconditionally")
			dialed = fwd.Unconditional
			*hops++
			continue
		}

		reg := s.getReg(calleeExt)
		if reg == nil {
			// Park orbit (GOBELISK-12): an unregistered number inside the
			// orbit is a slot, never a 404. An occupied slot connects the
			// caller to the parked party; an empty one parks the caller at
			// the server. A registered extension always rings normally, so
			// the orbit may overlap extension numbering.
			if slot, inOrbit := s.parkSlotFor(calleeExt); inOrbit {
				if parked := s.parkedAt(slot); parked != nil {
					s.log.Info().Str("callee", calleeExt).Str("parked", parked.CallID).Msg("picking up parked call")
					return &dialResult{
						target:    target,
						calleeExt: calleeExt,
						reg: &Registration{
							Ext:  calleeExt,
							Addr: parked.CallerAddr,
						},
						parked:   true,
						parkSlot: slot,
					}, true
				}
				return &dialResult{
					target:    target,
					calleeExt: calleeExt,
					parked:    true,
					parkSlot:  slot,
					parkEmpty: true,
				}, true
			}
			s.log.Warn().Str("callee", calleeExt).Msg("callee not registered")
			// Outbound routes (GOBELISK-21 / GOBELISK-22): plain dialed
			// numbers that no local endpoint owns fall through to the PSTN
			// via the cheapest matching trunk. Explicit dialplan mappings
			// (room/menu/meeting/service) stay local, and calls arriving
			// FROM a trunk never hairpin back out.
			if (target.Type == dialplan.TargetExtension || target.Type == dialplan.TargetUnknown) &&
				s.trunkByAddr(m.addr) == nil {
				if tr, number, routeName, ok := s.outboundDial(dialed); ok {
					s.log.Info().
						Str("dialed", dialed).
						Str("route", routeName).
						Str("trunk", tr.cfg.Name).
						Str("number", number).
						Msg("outbound route")
					return &dialResult{
						target:     target,
						calleeExt:  tr.cfg.Name,
						trunk:      tr,
						dialNumber: number,
					}, true
				}
			}
			m.respond(buildResponse(req, 404, "Not Found", nil, ""))
			return nil, false
		}

		// Busy: forward (CFB) instead of rejecting, or fall back to the
		// usual call-waiting gate. An attended-transfer takeover (Replaces,
		// RFC 3891 / GOBELISK-11) replaces the dialog: never busy for it.
		callWaiting := false
		if !hasReplaces(req) && s.extBusy(calleeExt) {
			if !s.callWaitingEnabled(calleeExt) {
				if fwd.Busy != "" {
					s.log.Info().Str("callee", calleeExt).Str("to", fwd.Busy).Msg("call forwarded (busy)")
					dialed = fwd.Busy
					*hops++
					continue
				}
				s.log.Info().Str("callee", calleeExt).Msg("callee busy, call waiting disabled")
				m.respond(buildResponse(req, 486, "Busy Here", nil, ""))
				return nil, false
			}
			callWaiting = true
		}

		// Side effects belong to the destination the call really reaches.
		if target.Type == dialplan.TargetRoom {
			s.rooms.Join(target.Value, dialed)
		}
		if target.Type == dialplan.TargetMenu {
			s.log.Info().Str("menu", target.Value).Msg("entering IVR menu")
		}

		return &dialResult{
			target:      target,
			calleeExt:   calleeExt,
			reg:         reg,
			callWaiting: callWaiting,
			fwd:         fwd,
		}, true
	}

	s.log.Warn().Str("dialed", dialed).Msg("call forwarding hop limit reached")
	m.respond(buildResponse(req, 508, "Loop Detected", nil, ""))
	return nil, false
}

// cancelForwardedInvite cancels a ringing leg this server forwarded. It
// reuses the INVITE's Via branch, because CANCEL is matched to the INVITE
// transaction by branch (RFC 3261 §9.1) (GOBELISK-15).
func (s *Server) cancelForwardedInvite(uri string, addr *net.UDPAddr, branch string, orig *sip.RawMessage) {
	serverVia := "SIP/2.0/UDP " + s.host + ":" + strconv.Itoa(s.port) + ";branch=" + branch
	msg := buildRequest("CANCEL", uri, orig, map[string]string{
		"Via": serverVia,
	}, "")
	s.log.Debug().Str("uri", uri).Str("branch", branch).Msg("cancelling unanswered leg")
	s.sendRaw(addr, msg)
}

// legTimeout is how long a single forwarded leg may ring when no
// no-answer forward is configured.
const legTimeout = 30 * time.Second
