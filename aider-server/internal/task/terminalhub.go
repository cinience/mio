package task

import (
	"sync"
	"time"
)

type TerminalHub struct {
	mu      sync.Mutex
	limit   int
	streams map[string]*terminalStream
}

type terminalStream struct {
	buffer      []byte
	startCursor int64
	cursor      int64
	subs        map[chan []byte]struct{}
	lastAppend  time.Time
}

func NewTerminalHub(limit int) *TerminalHub {
	if limit <= 0 {
		limit = 8192
	}
	return &TerminalHub{
		limit:   limit,
		streams: make(map[string]*terminalStream),
	}
}

func (h *TerminalHub) Append(taskID string, chunk []byte) []byte {
	if len(chunk) == 0 {
		return nil
	}
	h.mu.Lock()
	stream := h.ensureStream(taskID)
	stream.buffer = append(stream.buffer, chunk...)
	stream.lastAppend = time.Now()
	stream.cursor += int64(len(chunk))
	if len(stream.buffer) > h.limit {
		trim := len(stream.buffer) - h.limit
		stream.buffer = stream.buffer[trim:]
		stream.startCursor += int64(trim)
	}
	for ch := range stream.subs {
		select {
		case ch <- chunk:
		default:
		}
	}
	h.mu.Unlock()
	return chunk
}

func (h *TerminalHub) Subscribe(taskID string, fromCursor int64) (<-chan []byte, []byte, func()) {
	h.mu.Lock()
	stream := h.ensureStream(taskID)
	if fromCursor < stream.startCursor {
		fromCursor = stream.startCursor
	}
	if fromCursor > stream.cursor {
		fromCursor = stream.cursor
	}
	var backlog []byte
	offset := fromCursor - stream.startCursor
	if offset <= int64(len(stream.buffer)) {
		backlog = append([]byte(nil), stream.buffer[offset:]...)
	}
	ch := make(chan []byte, 16)
	stream.subs[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		if stream, ok := h.streams[taskID]; ok {
			delete(stream.subs, ch)
		}
		h.mu.Unlock()
		safeClose(ch)
	}
	return ch, backlog, unsubscribe
}

func (h *TerminalHub) LastActivity(taskID string) time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	stream, ok := h.streams[taskID]
	if !ok {
		return time.Time{}
	}
	return stream.lastAppend
}

func (h *TerminalHub) Cleanup(taskID string) {
	h.mu.Lock()
	stream, ok := h.streams[taskID]
	if ok {
		delete(h.streams, taskID)
	}
	h.mu.Unlock()
	if ok {
		for ch := range stream.subs {
			safeClose(ch)
		}
	}
}

func (h *TerminalHub) PruneInactive(cutoff time.Time) {
	h.mu.Lock()
	var stale []*terminalStream
	for id, stream := range h.streams {
		if !stream.lastAppend.IsZero() && stream.lastAppend.Before(cutoff) {
			delete(h.streams, id)
			stale = append(stale, stream)
		}
	}
	h.mu.Unlock()
	for _, stream := range stale {
		for ch := range stream.subs {
			safeClose(ch)
		}
	}
}

func (h *TerminalHub) ensureStream(taskID string) *terminalStream {
	stream, ok := h.streams[taskID]
	if !ok {
		stream = &terminalStream{buffer: make([]byte, 0, h.limit), subs: make(map[chan []byte]struct{})}
		h.streams[taskID] = stream
	}
	return stream
}

func safeClose(ch chan []byte) {
	defer func() {
		_ = recover()
	}()
	close(ch)
}
