package speech

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Options is the engine-specific configuration passed from the `speech.options`
// section of the gobelisk config file.
type Options map[string]any

// Factory builds an Engine from its options.
type Factory func(Options) (Engine, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]Factory{}
)

// Register makes an engine available under name. Engines usually register
// themselves from an init function in the AI side-project:
//
//	func init() { speech.Register("whisper-piper", New) }
//
// Register panics on an empty name, a nil factory, or a duplicate name, so
// programming errors are caught at startup.
func Register(name string, factory Factory) {
	name = strings.TrimSpace(name)
	if name == "" {
		panic("speech: cannot register an engine with an empty name")
	}
	if factory == nil {
		panic("speech: nil factory for engine " + name)
	}

	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	if _, exists := factories[name]; exists {
		panic("speech: engine already registered: " + name)
	}
	factories[name] = factory
}

// New builds the engine registered under name. An empty name (or "noop")
// returns Noop. Unknown names are reported together with the registered ones.
func New(name string, opts Options) (Engine, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "noop" {
		return Noop{}, nil
	}

	factoriesMu.RLock()
	factory, ok := factories[name]
	factoriesMu.RUnlock()
	if !ok {
		registered := Names()
		if len(registered) == 0 {
			return nil, fmt.Errorf("speech: unknown engine %q (no engines registered)", name)
		}
		return nil, fmt.Errorf("speech: unknown engine %q (registered: %s)", name, strings.Join(registered, ", "))
	}

	engine, err := factory(opts)
	if err != nil {
		return nil, fmt.Errorf("speech: engine %q: %w", name, err)
	}
	if engine == nil {
		return nil, fmt.Errorf("speech: engine %q returned nil", name)
	}
	return engine, nil
}

// Names returns the sorted names of all registered engines.
func Names() []string {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()

	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
