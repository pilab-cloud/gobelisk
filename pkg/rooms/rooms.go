package rooms

import "sync"

// Room represents a conference room.
type Room struct {
	Name         string
	Participants map[string]bool
}

// Manager tracks conference rooms.
type Manager struct {
	m     sync.Mutex
	rooms map[string]*Room
}

func NewManager() *Manager {
	return &Manager{rooms: make(map[string]*Room)}
}

func (m *Manager) Join(name, participant string) {
	m.m.Lock()
	defer m.m.Unlock()

	if m.rooms[name] == nil {
		m.rooms[name] = &Room{Name: name, Participants: make(map[string]bool)}
	}
	m.rooms[name].Participants[participant] = true
}

func (m *Manager) Leave(name, participant string) {
	m.m.Lock()
	defer m.m.Unlock()

	if r := m.rooms[name]; r != nil {
		delete(r.Participants, participant)
	}
}

// List returns room names.
func (m *Manager) List() []string {
	m.m.Lock()
	defer m.m.Unlock()
	out := make([]string, 0, len(m.rooms))
	for name := range m.rooms {
		out = append(out, name)
	}
	return out
}

func (m *Manager) Participants(name string) []string {
	m.m.Lock()
	defer m.m.Unlock()

	r := m.rooms[name]
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.Participants))
	for p := range r.Participants {
		out = append(out, p)
	}
	return out
}
