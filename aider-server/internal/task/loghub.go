package task

import (
	"sync"

	"aider-server/internal/taskmodel"
)

type LogHub struct {
	mu      sync.Mutex
	limit   int
	streams map[string]*logStream
}

type logStream struct {
	entries []taskmodel.LogEntry
	nextSeq int64
	subs    map[chan taskmodel.LogEntry]struct{}
}

func NewLogHub(limit int) *LogHub {
	if limit <= 0 {
		limit = 200
	}
	return &LogHub{
		limit:   limit,
		streams: make(map[string]*logStream),
	}
}

func (h *LogHub) Append(taskID string, entry taskmodel.LogEntry) taskmodel.LogEntry {
	h.mu.Lock()
	stream := h.ensureStream(taskID)
	if entry.Seq < 0 {
		entry.Seq = stream.nextSeq
	}
	if entry.Seq >= stream.nextSeq {
		stream.nextSeq = entry.Seq + 1
	}
	stream.entries = append(stream.entries, entry)
	if len(stream.entries) > h.limit {
		stream.entries = stream.entries[len(stream.entries)-h.limit:]
	}
	for ch := range stream.subs {
		select {
		case ch <- entry:
		default:
		}
	}
	h.mu.Unlock()
	return entry
}

func (h *LogHub) Subscribe(taskID string, fromSeq int64) (<-chan taskmodel.LogEntry, []taskmodel.LogEntry, func()) {
	h.mu.Lock()
	stream := h.ensureStream(taskID)
	backlog := make([]taskmodel.LogEntry, 0, len(stream.entries))
	for _, entry := range stream.entries {
		if entry.Seq >= fromSeq {
			backlog = append(backlog, entry)
		}
	}
	ch := make(chan taskmodel.LogEntry, 16)
	stream.subs[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		if stream, ok := h.streams[taskID]; ok {
			delete(stream.subs, ch)
		}
		h.mu.Unlock()
		close(ch)
	}
	return ch, backlog, unsubscribe
}

func (h *LogHub) ensureStream(taskID string) *logStream {
	stream, ok := h.streams[taskID]
	if !ok {
		stream = &logStream{entries: make([]taskmodel.LogEntry, 0, h.limit), subs: make(map[chan taskmodel.LogEntry]struct{})}
		h.streams[taskID] = stream
	}
	return stream
}
