package manager_api

import (
	"backend-server/internal/adapters/manager/types"
	"context"
	"testing"
	"time"
)

func TestMemoryCache_SaveAndGet(t *testing.T) {
	cache := types.NewMemoryCache(10, time.Second*2)
	mac := "42:32:13:b2:7b:f4"
	mem := &types.DeviceMemory{
		MacAddress:    mac,
		SummaryMemory: "test summary",
		LastUpdated:   time.Now(),
		Version:       1,
		Size:          123,
	}

	cache.Set(mac, mem)
	got, ok := cache.Get(mac)
	if !ok {
		t.Fatalf("expected memory to be saved, but not found")
	}
	if got.SummaryMemory != mem.SummaryMemory {
		t.Errorf("expected %s, got %s", mem.SummaryMemory, got.SummaryMemory)
	}

	// 测试覆盖已有
	mem2 := &types.DeviceMemory{
		MacAddress:    mac,
		SummaryMemory: "updated summary",
		LastUpdated:   time.Now(),
		Version:       2,
		Size:          456,
	}
	cache.Set(mac, mem2)
	got2, ok2 := cache.Get(mac)
	if !ok2 || got2.SummaryMemory != "updated summary" {
		t.Errorf("expected updated summary, got %+v", got2)
	}

	// 测试过期
	time.Sleep(3 * time.Second)
	_, ok3 := cache.Get(mac)
	if ok3 {
		t.Errorf("expected memory to expire, but still found")
	}
}

func TestService_SaveAndGetDeviceMemory(t *testing.T) {
	t.Skip("requires running manager-api server")

	// 请根据实际 manager-api 服务地址和 token 替换下面的配置
	cfg := &Config{
		Enabled:                    true,
		BaseURL:                    "http://xiaozhi.localhost/xiaozhi",     // manager-api 服务地址
		Secret:                     "cc36d3ca-b065-47ef-ae29-d34f7540abd1", // 如有鉴权请填写真实 token
		CacheTTLSeconds:            10,
		BatchSize:                  10,
		FlushIntervalSeconds:       10,
		TimeoutSeconds:             5,
		HealthCheckIntervalSeconds: 10,
	}

	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	mac := "42:32:13:b2:7b:f4"
	summary := "integration test summary memory"

	ctx := context.Background()
	err = svc.SaveDeviceMemory(ctx, mac, summary)
	if err != nil {
		t.Fatalf("SaveDeviceMemory failed: %v", err)
	}

	got, err := svc.GetDeviceMemory(ctx, mac)
	if err != nil {
		t.Fatalf("GetDeviceMemory failed: %v", err)
	}
	if got != summary {
		t.Errorf("expected %q, got %q", summary, got)
	}
}
