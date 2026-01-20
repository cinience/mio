package webrtc_vad

import (
	"fmt"
	"sync"

	"backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"
	util "backend-server/internal/shared"
)

var (
	vadPool    *WebRTCVADPool
	vadPoolMu  sync.RWMutex
	vadPoolSig string
)

func AcquireVAD(config map[string]interface{}) (inter.VAD, error) {
	pool, err := ensureVADPool(config)
	if err != nil {
		return nil, err
	}
	return pool.AcquireVAD()
}

func ReleaseVAD(vad inter.VAD) error {
	vadPoolMu.RLock()
	pool := vadPool
	vadPoolMu.RUnlock()

	if pool != nil {
		return pool.ReleaseVAD(vad)
	}
	return nil
}

func ensureVADPool(config map[string]interface{}) (*WebRTCVADPool, error) {
	poolConfig := getPoolConfigFromMap(config)
	vadConfig := getVadConfigFromMap(config)
	signature := buildVADPoolSignature(vadConfig, poolConfig)

	vadPoolMu.Lock()
	defer vadPoolMu.Unlock()

	if vadPool != nil && signature == vadPoolSig {
		return vadPool, nil
	}

	if vadPool != nil {
		if err := vadPool.Close(); err != nil {
			log.Warnf("closing previous WebRTC VAD pool failed: %v", err)
		}
		vadPool = nil
		vadPoolSig = ""
	}

	pool, err := NewWebRTCVADPool(vadConfig, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create WebRTC VAD pool: %w", err)
	}

	vadPool = pool
	vadPoolSig = signature
	return vadPool, nil
}

func buildVADPoolSignature(vadConfig WebRTCVADConfig, poolConfig *util.PoolConfig) string {
	if poolConfig == nil {
		poolConfig = util.DefaultConfig()
	}

	return fmt.Sprintf("sr:%d|mode:%d|noise:%f|min:%d|max:%d|idle:%d|borrow:%t|return:%t",
		vadConfig.SampleRate,
		vadConfig.Mode,
		vadConfig.NoiseFloor,
		poolConfig.MinSize,
		poolConfig.MaxSize,
		poolConfig.MaxIdle,
		poolConfig.ValidateOnBorrow,
		poolConfig.ValidateOnReturn,
	)
}
