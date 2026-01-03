package session

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

const sessionCookieName = "corvus_session"
const sessionDuration = 24 * time.Hour

// Session represents a user session
type Session struct {
	ID        string
	Data      map[string]interface{}
	ExpiresAt time.Time
	mu        sync.RWMutex
}

// Set stores a value in the session
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Data[key] = value
}

// Get retrieves a value from the session
func (s *Session) Get(key string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Data[key]
}

// Manager manages sessions
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewManager creates a new session manager
func NewManager() *Manager {
	m := &Manager{
		sessions: make(map[string]*Session),
	}

	// Start cleanup goroutine
	go m.cleanup()

	return m
}

// cleanup removes expired sessions
func (m *Manager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		for id, session := range m.sessions {
			if time.Now().After(session.ExpiresAt) {
				delete(m.sessions, id)
			}
		}
		m.mu.Unlock()
	}
}

// New creates a new session
func (m *Manager) New() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := generateSessionID()
	session := &Session{
		ID:        id,
		Data:      make(map[string]interface{}),
		ExpiresAt: time.Now().Add(sessionDuration),
	}

	m.sessions[id] = session
	return session
}

// Get retrieves a session by ID from cookie
func (m *Manager) Get(r *http.Request) *Session {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[cookie.Value]
	if !exists || time.Now().After(session.ExpiresAt) {
		return nil
	}

	return session
}

// Save saves the session and sets the cookie
func (m *Manager) Save(w http.ResponseWriter, session *Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
}

// Destroy removes a session
func (m *Manager) Destroy(w http.ResponseWriter, session *Session) {
	m.mu.Lock()
	delete(m.sessions, session.ID)
	m.mu.Unlock()

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// generateSessionID generates a random session ID
func generateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
