package session

import (
	"sync"
	"time"

	log "backend-server/internal/infrastructure/logger"
)

type SessionMonitor struct {
	mu            sync.RWMutex
	sessions      map[*ChatSession]struct{}
	checkInterval time.Duration
	timeout       time.Duration
	idleTimeout   time.Duration
	startOnce     sync.Once
	stopOnce      sync.Once
	stopCh        chan struct{}
}

func NewSessionMonitor(checkInterval, timeout time.Duration) *SessionMonitor {
	if checkInterval <= 0 {
		checkInterval = 30 * time.Second
	}
	if timeout <= 0 {
		timeout = time.Minute * 30
	}
	idleTimeout := 2 * time.Minute
	if timeout > 0 && timeout < idleTimeout {
		idleTimeout = timeout
	}
	return &SessionMonitor{
		sessions:      make(map[*ChatSession]struct{}),
		checkInterval: checkInterval,
		timeout:       timeout,
		idleTimeout:   idleTimeout,
		stopCh:        make(chan struct{}),
	}
}

func (m *SessionMonitor) start() {
	m.startOnce.Do(func() {
		go m.loop()
	})
}

func (m *SessionMonitor) Add(session *ChatSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	m.sessions[session] = struct{}{}
	m.mu.Unlock()
	m.start()
}

func (m *SessionMonitor) Remove(session *ChatSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	delete(m.sessions, session)
	m.mu.Unlock()
}

func (m *SessionMonitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}

func (m *SessionMonitor) loop() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.checkSessions()
		case <-m.stopCh:
			return
		}
	}
}

func (m *SessionMonitor) checkSessions() {
	m.mu.RLock()
	sessions := make([]*ChatSession, 0, len(m.sessions))
	for sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	m.mu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	now := time.Now()
	statsList := make([]map[string]interface{}, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		if sess.closed.Load() {
			m.Remove(sess)
			continue
		}
		last := sess.lastActivityTime.Load()
		if last == 0 {
			statsList = append(statsList, sess.monitorSnapshot(now))
			continue
		}
		stats := sess.monitorSnapshot(now)
		if m.idleTimeout > 0 {
			status, _ := stats["status"].(string)
			if status == "listening" || status == "listenStop" {
				if last > 0 && now.Sub(time.UnixMilli(last)) > m.idleTimeout {
					deviceID := ""
					if sess.clientState != nil {
						deviceID = sess.clientState.DeviceID
					}
					stats["action"] = "reclaim"
					log.Infof("监听会话超过 %s 未活跃，自动回收，设备 %s", m.idleTimeout, deviceID)
					go sess.Close()
				}
			}
		}
		if now.Sub(time.UnixMilli(last)) > m.timeout {
			deviceID := ""
			if sess.clientState != nil {
				deviceID = sess.clientState.DeviceID
			}
			stats["action"] = "close"
			log.Infof("会话超过 %s 未活跃，自动关闭，设备 %s", m.timeout, deviceID)
			go sess.Close()
		}
		statsList = append(statsList, stats)
	}

	if len(statsList) > 0 {
		log.Infof("SessionMonitor 当前会话数: %d", len(statsList))
		for _, stats := range statsList {
			log.Infof("SessionMonitor 会话状态: %+v", stats)
		}
	}
}
