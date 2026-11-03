package dialplan

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/ivr"
)

// ApplyConfig parses a decision-language config and populates dp and ivrMgr.
func ApplyConfig(dp *DialPlan, ivrMgr *ivr.Manager, text string) error {
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