package ivr

import "sync"

// Menu is a simple IVR menu mapping DTMF digits to targets.
type Menu struct {
	Name    string
	Prompt  string
	Options map[string]string // digit -> dial string or action
}

// PINCollection describes a PIN entry step.
type PINCollection struct {
	Length   int
	Meeting  string
	ValidPIN string
}

// Session tracks per-call IVR state.
type Session struct {
	CallID     string
	Menu       string
	Collecting bool
	PIN        string
	PINLength  int
	Meeting    string
	ValidPIN   string
	MeetingID  string
	SessionID  string
}

// Manager holds IVR menus and sessions.
type Manager struct {
	mu       sync.Mutex
	menus    map[string]*Menu
	pins     map[string]*PINCollection // meeting -> pin collection
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{
		menus:    make(map[string]*Menu),
		pins:     make(map[string]*PINCollection),
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) Add(menu *Menu) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.menus[menu.Name] = menu
}

func (m *Manager) Get(name string) (*Menu, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	menu, ok := m.menus[name]
	return menu, ok
}

func (m *Manager) Resolve(menuName, digit string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	menu, ok := m.menus[menuName]
	if !ok {
		return "", false
	}
	target, ok := menu.Options[digit]
	return target, ok
}

// StartPINCollection begins collecting a PIN for a meeting.
func (m *Manager) StartPINCollection(callID, meeting string, length int, validPIN string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[callID] = &Session{
		CallID:     callID,
		Collecting: true,
		PINLength:  length,
		Meeting:    meeting,
		ValidPIN:   validPIN,
	}
}

// HandleDigit appends a digit to the PIN buffer. Returns (complete bool, pin string).
func (m *Manager) HandleDigit(callID, digit string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[callID]
	if s == nil || !s.Collecting {
		return false, ""
	}
	if digit == "#" {
		s.Collecting = false
		return true, s.PIN
	}
	s.PIN += digit
	if s.PINLength > 0 && len(s.PIN) >= s.PINLength {
		s.Collecting = false
		return true, s.PIN
	}
	return false, s.PIN
}

// GetSession returns the session for a call.
func (m *Manager) GetSession(callID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[callID]
	return s, ok
}

// EndSession removes a session.
func (m *Manager) EndSession(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, callID)
}
