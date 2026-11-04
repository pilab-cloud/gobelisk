package pbx

import (
	"regexp"

	"github.com/devopshaven/gobelisk/pkg/config"
)

// fromUserRe matches the user part of the SIP URI in a From-style header:
// "sip:user@host", "<sip:user@host>;tag=.." or "\"Name\" <sip:...>".
var fromUserRe = regexp.MustCompile(`(?i)(sips?:)[^@;\s>]+(@)`)

// setFromUser replaces the user part of the SIP URI in a From header and
// keeps everything else — display name, host, parameters and the dialog
// tag — untouched, so dialog matching keeps working (GOBELISK-23).
func setFromUser(from, user string) string {
	if from == "" || user == "" {
		return from
	}
	loc := fromUserRe.FindStringSubmatchIndex(from)
	if loc == nil {
		return from
	}
	// loc[3]:loc[4] is the user part between the scheme and the @.
	return from[:loc[3]] + user + from[loc[4]:]
}

// inboundCIDFrom applies an inbound route's caller ID manipulation to the
// From header of the request: strip leading digits, then prepend a prefix
// ("06301234567" → strip 2 → prefix "+36" → "+36301234567"). It returns ""
// when the route does not rewrite the caller ID, or when the result would
// be an empty user part — the original From is then presented unchanged
// (GOBELISK-23).
func inboundCIDFrom(from string, r config.InboundRoute) string {
	if from == "" || (r.CIDStrip <= 0 && r.CIDPrefix == "") {
		return ""
	}
	user := userFromURI(from)
	if user == "" {
		return ""
	}
	if r.CIDStrip > 0 {
		if r.CIDStrip >= len(user) {
			user = ""
		} else {
			user = user[r.CIDStrip:]
		}
	}
	user = r.CIDPrefix + user
	if user == "" {
		return ""
	}
	return setFromUser(from, user)
}

// fromForLeg returns the From header to present on a request that is being
// forwarded from fromLeg to the opposite leg: requests traveling toward the
// callee carry the caller ID that leg was shown when the dialog was created
// (GOBELISK-23); requests toward the caller keep their sender's own From and
// return "" (meaning no override).
func fromForLeg(call *Call, fromLeg string) string {
	if call == nil || fromLeg != "caller" {
		return ""
	}
	return call.calleeFrom
}
