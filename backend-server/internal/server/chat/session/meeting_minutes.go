package session

import (
	"strings"
	"sync"
	"time"

	"backend-server/internal/domain/meetingminutes"
)

type meetingMinutesState struct {
	mu          sync.Mutex
	active      bool
	startedAt   time.Time
	startPhrase string
	stopPhrase  string
	entries     []meetingminutes.Entry
}

func newMeetingMinutesState(startPhrase, stopPhrase string) *meetingMinutesState {
	return &meetingMinutesState{
		startPhrase: strings.TrimSpace(startPhrase),
		stopPhrase:  strings.TrimSpace(stopPhrase),
		entries:     make([]meetingminutes.Entry, 0, 32),
	}
}

func (m *meetingMinutesState) Start(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active {
		return meetingminutes.ErrAlreadyActive
	}
	m.active = true
	m.startedAt = now
	m.entries = m.entries[:0]
	return nil
}

func (m *meetingMinutesState) Stop(now time.Time) (*meetingminutes.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return nil, meetingminutes.ErrNotActive
	}
	m.active = false
	snapshot := &meetingminutes.Snapshot{
		StartedAt: m.startedAt,
		EndedAt:   now,
		Entries:   append([]meetingminutes.Entry(nil), m.entries...),
	}
	return snapshot, nil
}

func (m *meetingMinutesState) Append(text string, at time.Time) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return
	}
	if m.shouldIgnore(trimmed) {
		return
	}
	m.entries = append(m.entries, meetingminutes.Entry{
		Text: trimmed,
		At:   at,
	})
}

func (m *meetingMinutesState) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *meetingMinutesState) shouldIgnore(text string) bool {
	if m.startPhrase != "" && strings.EqualFold(text, m.startPhrase) {
		return true
	}
	if m.stopPhrase != "" && strings.EqualFold(text, m.stopPhrase) {
		return true
	}
	return false
}
