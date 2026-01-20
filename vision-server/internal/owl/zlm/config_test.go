package zlm

import (
	"fmt"
	"os"
	"testing"
)

func TestEngine_GetServerConfig(t *testing.T) {
	url := os.Getenv("ZLM_TEST_ADDR")
	if url == "" {
		t.Skip("ZLM_TEST_ADDR not set")
	}
	e := NewEngine().SetConfig(Config{URL: url, Secret: "OHvo86N9Ww6V8mHPWMisxNgkb8dvqAV420241107"})
	out, err := e.GetServerConfig()
	if err != nil {
		t.Errorf("Engine.GetServerConfig() error = %v", err)
		return
	}
	fmt.Printf("%+v", out)
}

func TestGetSnap(t *testing.T) {
	url := os.Getenv("ZLM_TEST_ADDR")
	if url == "" {
		t.Skip("ZLM_TEST_ADDR not set")
	}
	const link = "rtmp://localhost:1935/rtp/che1ml5"
	b, err := NewEngine().SetConfig(
		Config{URL: url, Secret: "jvRqCAzEg7AszBi4gm1cfhwXpmnVmJMG"},
	).GetSnap(GetSnapRequest{URL: link, TimeoutSec: 50, ExpireSec: 10})
	if err != nil {
		t.Errorf("Engine.GetServerConfig() error = %v", err)
	}

	fmt.Println(len(b))
	os.WriteFile("snap.jpg", b, 0o644)
	fmt.Println(string(b))

	fmt.Println(md5FromBytes(b))
}
