package dialplan

import "strings"

// TargetType describes where a dialed string routes.
type TargetType string

const (
	TargetExtension TargetType = "extension"
	TargetRoom      TargetType = "room"
	TargetMenu     TargetType = "menu"
	TargetMeeting  TargetType = "meeting"
	TargetService  TargetType = "service"
	TargetUnknown   TargetType = "unknown"
)

// Target is the resolved destination of a dialed string.
type Target struct {
	Type    TargetType
	Value   string // extension number / room name / menu name
	Original string
}

// RouteRule is a single decision-language route.
type RouteRule struct {
	Pattern string
	Action  string // e.g. "ext 100", "room lobby", "menu main", "hangup", "repeat"
}

// DialPlan maps dialed strings to targets.
type DialPlan struct {
	// Extensions maps a dialed number to an endpoint extension.
	Extensions map[string]string
	// Rooms maps a room name to an endpoint extension that hosts it.
	Rooms map[string]string
	// Menus maps a menu name to an endpoint extension (IVR agent).
	Menus map[string]string
	// Meetings maps a meeting name to an endpoint extension (meeting gateway).
	Meetings map[string]string
	// Rules are ordered decision-language routes.
	Rules []RouteRule
}

// New returns a DialPlan with sensible defaults.
func New() *DialPlan {
	return &DialPlan{
		Extensions: make(map[string]string),
		Rooms:      make(map[string]string),
		Menus:      make(map[string]string),
		Meetings:   make(map[string]string),
		Rules:      []RouteRule{},
	}
}

// Parse resolves a dialed string into a Target.
func (d *DialPlan) Parse(input string) Target {
	input = strings.TrimSpace(input)
	t := Target{Original: input}

	// Decision-language rules (first match wins)
	for _, r := range d.Rules {
		if matchPattern(r.Pattern, input) {
			// Action like "ext 100"
			fields := strings.Fields(r.Action)
			if len(fields) > 0 {
				switch fields[0] {
				case "ext":
					t.Type = TargetExtension
					if len(fields) > 1 {
						t.Value = fields[1]
					}
					return t
				case "room":
					t.Type = TargetRoom
					if len(fields) > 1 {
						t.Value = fields[1]
					}
					return t
				case "menu":
					t.Type = TargetMenu
					if len(fields) > 1 {
						t.Value = fields[1]
					}
					return t
				case "meeting":
					t.Type = TargetMeeting
					if len(fields) > 1 {
						t.Value = fields[1]
					}
					return t
				case "hangup":
					t.Type = TargetService
					t.Value = "hangup"
					return t
				case "repeat":
					t.Type = TargetService
					t.Value = "repeat"
					return t
				}
			}
		}
	}

	// PIN entry: *<digits># or *<digits>
	if strings.HasPrefix(input, "*") {
		digits := strings.TrimLeft(input, "*")
		digits = strings.TrimRight(digits, "#")
		if digits != "" && isDigits(digits) {
			t.Type = TargetService
			t.Value = "pin:" + digits
			return t
		}
		t.Type = TargetService
		t.Value = "star"
		return t
	}

	if input == "#" {
		t.Type = TargetService
		t.Value = "hash"
		return t
	}

	switch {
	case strings.HasPrefix(input, "room:"):
		name := strings.TrimPrefix(input, "room:")
		t.Type = TargetRoom
		t.Value = name
		return t
	case strings.HasPrefix(input, "menu:"):
		name := strings.TrimPrefix(input, "menu:")
		t.Type = TargetMenu
		t.Value = name
		return t
	case strings.HasPrefix(input, "service:"):
		name := strings.TrimPrefix(input, "service:")
		t.Type = TargetService
		t.Value = name
		return t
	}

	// Numeric handling
	if isDigits(input) {
		switch {
		case strings.HasPrefix(input, "8"):
			// 8xxx -> room shortcut
			t.Type = TargetRoom
			t.Value = input
		case strings.HasPrefix(input, "9"):
			// 9xxx -> menu shortcut
			t.Type = TargetMenu
			t.Value = input
		default:
			t.Type = TargetExtension
			t.Value = input
		}
		return t
	}

	t.Type = TargetUnknown
	t.Value = input
	return t
}

// ResolveExtension returns the endpoint extension for a target, if mapped.
func (d *DialPlan) ResolveExtension(t Target) (string, bool) {
	switch t.Type {
	case TargetExtension:
		if v, ok := d.Extensions[t.Value]; ok {
			return v, true
		}
		return t.Value, true
	case TargetRoom:
		if v, ok := d.Rooms[t.Value]; ok {
			return v, true
		}
		// try numeric room mapping
		if v, ok := d.Rooms["room:"+t.Value]; ok {
			return v, true
		}
	case TargetMenu:
		if v, ok := d.Menus[t.Value]; ok {
			return v, true
		}
		if v, ok := d.Menus["menu:"+t.Value]; ok {
			return v, true
		}
	case TargetMeeting:
		if v, ok := d.Meetings[t.Value]; ok {
			return v, true
		}
		if v, ok := d.Meetings["meeting:"+t.Value]; ok {
			return v, true
		}
	}
	return "", false
}

// matchPattern supports exact match and trailing "*" wildcard.
func matchPattern(pattern, input string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(input, prefix)
	}
	return pattern == input
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}