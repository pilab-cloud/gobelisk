package sip

import (
	"sync"
)

type SessionManager struct {
	m        sync.Mutex
	sessions map[string]*DeviceManager
}

// AddSession adds a new online device
func (s *SessionManager) AddSession(ext string) {
	s.m.Lock()
	defer s.m.Unlock()

	if s.sessions == nil {
		s.sessions = make(map[string]*DeviceManager)
	}

	if _, ok := s.sessions[ext]; !ok {
		s.sessions[ext] = &DeviceManager{}
	}
}

// IsOnline
func (s *SessionManager) IsRegistred(extension string) bool {
	s.m.Lock()
	defer s.m.Unlock()

	_, ok := s.sessions[extension]

	return ok
}