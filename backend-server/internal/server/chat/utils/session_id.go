package utils

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var processRandomStr string
var sessionCounter int64

func init() {
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		processRandomStr = "session"
	} else {
		processRandomStr = strings.ToLower(base64.URLEncoding.EncodeToString(randomBytes)[:6])
	}
}

func GenerateClientSessionID() string {
	currentTime := time.Now().Format("20060102150405")
	counter := atomic.AddInt64(&sessionCounter, 1)
	return fmt.Sprintf("%s-%s-%d", processRandomStr, currentTime, counter)
}
