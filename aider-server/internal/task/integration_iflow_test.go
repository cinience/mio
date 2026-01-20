//go:build integration

package task

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"aider-server/internal/store"
	"aider-server/internal/task/adapters"
	"aider-server/internal/taskmodel"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestIflowInteractiveStaysRunning(t *testing.T) {
	path, err := exec.LookPath("iflow")
	if err != nil {
		t.Skip("iflow not found in PATH")
	}

	proc, err := StartProcess(ExecSpec{
		Command: path,
		Env:     BuildEnv(map[string]string{}, map[string]string{}),
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("start iflow: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = proc.Interrupt(ctx)
	}()

	var (
		mu           sync.Mutex
		lastActivity time.Time
		outBuf       bytes.Buffer
	)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := proc.OutputReader().Read(buf)
			if n > 0 {
				mu.Lock()
				lastActivity = time.Now()
				outBuf.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	waitForOutput := time.After(10 * time.Second)
	for {
		mu.Lock()
		seen := !lastActivity.IsZero()
		mu.Unlock()
		if seen {
			break
		}
		select {
		case <-waitForOutput:
			t.Fatalf("no output from iflow within timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}

	if _, err := proc.Write([]byte("what time is it?")); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	sendErr := WaitAndSendEnter(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return lastActivity
	}, func(data []byte) error {
		_, err := proc.Write(data)
		return err
	}, 20*time.Second, 500*time.Millisecond)
	if sendErr != nil {
		t.Fatalf("send enter: %v", sendErr)
	}

	exitCh := make(chan error, 1)
	go func() {
		exitCh <- proc.Wait()
	}()
	select {
	case err := <-exitCh:
		mu.Lock()
		output := outBuf.String()
		mu.Unlock()
		t.Fatalf("iflow exited early: %v\noutput:\n%s", err, output)
	case <-time.After(2 * time.Second):
	}
}

func TestIflowManagerPromptExecutes(t *testing.T) {
	path, err := exec.LookPath("iflow")
	if err != nil {
		t.Skip("iflow not found in PATH")
	}

	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	taskStore := store.NewRedisStore(client, "aider")

	registry := adapters.NewRegistry()
	registry.Register(adapters.Adapter{
		Name:    "iflow",
		Command: path,
		Args:    []string{},
	})

	manager := NewManager(taskStore, registry, []string{"iflow"}, t.TempDir(), time.Minute, CodexProtocolConfig{})
	ctx := context.Background()
	taskItem, err := manager.CreateTask(ctx, taskmodel.CreateRequest{
		Client:      "iflow",
		Prompt:      "现在什么时间？",
		Interactive: true,
	}, map[string]string{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	defer func() {
		interruptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = manager.Interrupt(interruptCtx, taskItem.ID)
	}()

	ch, backlog, unsubscribe := manager.SubscribeTerminal(taskItem.ID, 0)
	defer unsubscribe()

	var (
		mu      sync.Mutex
		outBuf  bytes.Buffer
		stopped = make(chan struct{})
	)
	outBuf.Write(backlog)
	go func() {
		defer close(stopped)
		for chunk := range ch {
			mu.Lock()
			outBuf.Write(chunk)
			mu.Unlock()
		}
	}()

	outputFetcher := func() string {
		mu.Lock()
		defer mu.Unlock()
		return outBuf.String()
	}
	if err := waitForOutputContains(outputFetcher, "现在什么时间？", 30*time.Second); err != nil {
		if strings.Contains(outputFetcher(), "Waiting for auth") {
			t.Skip("iflow requires auth; skipping interactive prompt test")
		}
		t.Fatalf("prompt not echoed: %v\noutput:\n%s", err, safeOutput(&mu, &outBuf))
	}

	initialLen := func() int {
		mu.Lock()
		defer mu.Unlock()
		return outBuf.Len()
	}()
	if err := waitForOutputLen(func() int {
		mu.Lock()
		defer mu.Unlock()
		return outBuf.Len()
	}, initialLen, 90*time.Second); err != nil {
		t.Fatalf("no response output after prompt: %v\noutput:\n%s", err, safeOutput(&mu, &outBuf))
	}
}

func waitForOutputContains(fetch func() string, needle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(fetch(), needle) {
			return nil
		}
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForOutputLen(fetch func() int, baseline int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if fetch() > baseline {
			return nil
		}
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func safeOutput(mu *sync.Mutex, buf *bytes.Buffer) string {
	mu.Lock()
	defer mu.Unlock()
	if buf.Len() > 8000 {
		return buf.String()[buf.Len()-8000:]
	}
	return buf.String()
}
