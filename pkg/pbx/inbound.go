package pbx

import "github.com/devopshaven/gobelisk/pkg/config"

// inboundDestination rewrites the dialed number when the request arrives on
// a provisioned trunk and matches an inbound route (GOBELISK-20), and returns
// the matched route so the caller can apply its caller ID manipulation
// (GOBELISK-23). Returns false when the source is not a trunk or no route
// matches — the call then resolves normally (a DID may even be a local
// extension).
func (s *Server) inboundDestination(m sipMsg, dialed string) (string, config.InboundRoute, bool) {
	if dialed == "" || m.addr == nil {
		return "", config.InboundRoute{}, false
	}
	src := s.trunkByAddr(m.addr)
	if src == nil {
		return "", config.InboundRoute{}, false
	}
	for _, r := range s.cfg.InboundRoutes {
		if r.Trunk != "" && r.Trunk != src.cfg.Name {
			continue
		}
		if r.Match(dialed) && r.Destination != "" {
			s.log.Info().
				Str("did", dialed).
				Str("route", r.Name).
				Str("trunk", src.cfg.Name).
				Str("destination", r.Destination).
				Msg("inbound route")
			return r.Destination, r, true
		}
	}
	return "", config.InboundRoute{}, false
}
