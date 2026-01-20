package state

import (
	"fmt"
	"sync"
	"sync/atomic"

	"backend-server/internal/domain/vad"
	vadinter "backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"
)

type Vad struct {
	lock sync.RWMutex
	// VAD 提供者
	VadProvider vadinter.VAD

	IdleDuration int64 // 空闲时间, 单位: ms
}

func (v *Vad) AddIdleDuration(idleDuration int64) int64 {
	return atomic.AddInt64(&v.IdleDuration, idleDuration)
}

func (v *Vad) GetIdleDuration() int64 {
	return atomic.LoadInt64(&v.IdleDuration)
}

func (v *Vad) ResetIdleDuration() {
	atomic.StoreInt64(&v.IdleDuration, 0)
}

// IsInitialized 线程安全地检查 VAD 是否已初始化
func (v *Vad) IsInitialized() bool {
	v.lock.RLock()
	defer v.lock.RUnlock()
	return v.VadProvider != nil
}

// GetProvider 线程安全地获取 VAD provider 的引用
func (v *Vad) GetProvider() vadinter.VAD {
	v.lock.RLock()
	defer v.lock.RUnlock()
	return v.VadProvider
}

func (v *Vad) Init(provider string, config map[string]interface{}) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	if v.VadProvider != nil {
		if err := vad.ReleaseVAD(v.VadProvider); err != nil {
			log.Warnf("释放旧VAD实例失败: %v", err)
		}
		v.VadProvider = nil
	}

	vadProvider, err := vad.AcquireVAD(provider, config)
	if err != nil {
		return fmt.Errorf("创建 VAD 提供者失败: %v", err)
	}

	vadProvider.Reset()
	v.VadProvider = vadProvider
	return nil
}

func (v *Vad) ResetVad() error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if v.VadProvider != nil {
		v.VadProvider.Reset()
		return nil
	}
	return fmt.Errorf("vad provider is nil")
}

func (v *Vad) IsVADExt(pcmData []float32, sampleRate int, frameSize int) (bool, error) {
	v.lock.Lock()
	defer v.lock.Unlock()
	if v.VadProvider != nil {
		return v.VadProvider.IsVADExt(pcmData, sampleRate, frameSize)
	}
	return false, nil
}

func (v *Vad) Reset() error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if v.VadProvider != nil {
		if err := vad.ReleaseVAD(v.VadProvider); err != nil {
			log.Warnf("释放VAD实例失败: %v", err)
		}
		v.VadProvider = nil //置nil
	}
	v.ResetIdleDuration()
	return nil
}
