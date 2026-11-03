package dialplan

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/ivr"
)

// ApplyConfig parses a decision-language config and populates dp and ivrMgr.
// Routes and ring groups are replaced, so reloading a config does not
// accumulate stale entries.
func ApplyConfig(dp *DialPlan, ivrMgr *ivr.Manager, text string) error {
	dp.Rules = nil
	dp.RingGroups = make(map[string]*RingGroup)

	scanner := bufio.NewScanner(strings.NewReader(text))
	var currentMenu *ivr.Menu
	inMenu := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "menu ") && strings.HasSuffix(line, "{") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "menu "), "{"))
			currentMenu = &ivr.Menu{Name: name, Options: make(map[string]string)}
			inMenu = true
			continue
		}

		if line == "}" {
			if inMenu && currentMenu != nil {
				ivrMgr.Add(currentMenu)
				// map menu to an IVR agent if not already
				inMenu = false
				currentMenu = nil
			}
			continue
		}

		if inMenu && currentMenu != nil {
			// prompt "..."
			if strings.HasPrefix(line, "prompt ") {
				val := strings.TrimPrefix(line, "prompt ")
				val = strings.Trim(val, "\"")
				currentMenu.Prompt = val
				continue
			}
			// pin <code> -> <action>
			if strings.HasPrefix(line, "pin ") {
				fields := strings.Fields(line)
				// pin CODE -> action...
				if len(fields) >= 4 && fields[2] == "->" {
					code := fields[1]
					action := strings.Join(fields[3:], " ")
					currentMenu.Options["pin:"+code] = action
				}
				continue
			}
			// <key> -> <action>
			if strings.Contains(line, "->") {
				parts := strings.SplitN(line, "->", 2)
				key := strings.TrimSpace(parts[0])
				action := strings.TrimSpace(parts[1])
				currentMenu.Options[key] = action
				continue
			}
			continue
		}

		// meeting <name> -> ext <num>  (shorthand mapping)
		if strings.HasPrefix(line, "meeting ") && strings.Contains(line, "->") {
			fields := strings.Fields(line)
			idx := -1
			for i, f := range fields {
				if f == "->" {
					idx = i
					break
				}
			}
			if idx > 0 {
				name := fields[1]
				action := strings.Join(fields[idx+1:], " ")
				if afields := strings.Fields(action); len(afields) > 1 && afields[0] == "ext" {
					dp.Meetings[name] = afields[1]
				}
			}
			continue
		}

		// ringgroup <name> -> <ext> [<ext>...]
		// ringgroup <name> strategy <strategy> -> <ext> [<ext>...]
		if strings.HasPrefix(line, "ringgroup ") {
			group, err := parseRingGroup(line)
			if err != nil {
				return err
			}
			dp.RingGroups[group.Name] = group
			continue
		}

		// route <pattern> -> <action>
		if strings.HasPrefix(line, "route ") {
			fields := strings.Fields(line)
			// route PATTERN -> action...
			// find ->
			idx := -1
			for i, f := range fields {
				if f == "->" {
					idx = i
					break
				}
			}
			if idx > 0 && idx+1 < len(fields) {
				pattern := fields[1]
				action := strings.Join(fields[idx+1:], " ")
				dp.Rules = append(dp.Rules, RouteRule{Pattern: pattern, Action: action})
				continue
			}
		}

		// menu <name> -> ext <num>  (shorthand mapping)
		if strings.HasPrefix(line, "menu ") && strings.Contains(line, "->") {
			fields := strings.Fields(line)
			idx := -1
			for i, f := range fields {
				if f == "->" {
					idx = i
					break
				}
			}
			if idx > 0 {
				name := fields[1]
				action := strings.Join(fields[idx+1:], " ")
				if afields := strings.Fields(action); len(afields) > 1 && afields[0] == "ext" {
					dp.Menus[name] = afields[1]
				}
				if afields := strings.Fields(action); len(afields) > 1 && afields[0] == "meeting" {
					// meeting <name> -> ext <num> handled by separate meeting line
				}
			}
			continue
		}

		return fmt.Errorf("unrecognized line: %s", line)
	}

	return scanner.Err()
}

// parseRingGroup parses a ringgroup config line:
//
//	ringgroup sales -> 100 101 102
//	ringgroup sales strategy round-robin -> 100, 101, 102
//
// The optional "ext" keyword in the member list is accepted for readability.
func parseRingGroup(line string) (*RingGroup, error) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return nil, fmt.Errorf("ringgroup needs a name and members: %s", line)
	}

	name := fields[1]
	strategy := StrategySimultaneous

	rest := fields[2:]
	if len(rest) >= 2 && rest[0] == "strategy" {
		strategy = RingStrategy(rest[1])
		rest = rest[2:]
	}

	arrow := -1
	for i, f := range rest {
		if f == "->" {
			arrow = i
			break
		}
	}
	if arrow < 0 || arrow+1 >= len(rest) {
		return nil, fmt.Errorf("ringgroup is missing '-> members': %s", line)
	}

	switch strategy {
	case StrategySimultaneous, StrategyRoundRobin, StrategyLeastRecent:
	default:
		return nil, fmt.Errorf("ringgroup %s: unknown strategy %q", name, strategy)
	}

	var members []string
	for _, m := range rest[arrow+1:] {
		m = strings.Trim(strings.TrimSpace(m), ",")
		if m == "" || m == "ext" {
			continue
		}
		members = append(members, m)
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("ringgroup %s has no members", name)
	}

	return &RingGroup{Name: name, Members: members, Strategy: strategy}, nil
}
