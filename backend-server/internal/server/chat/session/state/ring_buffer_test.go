package state

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRingAudioChannel_Basic(t *testing.T) {
	ring := NewRingAudioChannel(3)
	frames := [][]float32{
		{1, 2},
		{3, 4},
		{5, 6},
	}
	for _, frame := range frames {
		if ok := ring.Write(frame); !ok {
			t.Fatalf("write failed for frame %v", frame)
		}
	}
	for i := 0; i < len(frames); i++ {
		got, ok := ring.Read()
		if !ok {
			t.Fatalf("expected frame %d", i)
		}
		if len(got) != len(frames[i]) {
			t.Fatalf("unexpected frame length: got %d want %d", len(got), len(frames[i]))
		}
	}
	written, read, dropped, usage := ring.Stats()
	if written != 3 || read != 3 || dropped != 0 {
		t.Fatalf("unexpected stats: %d %d %d", written, read, dropped)
	}
	if usage != 0 {
		t.Fatalf("expected empty usage, got %f", usage)
	}
}

func TestRingAudioChannel_OverwriteOldest(t *testing.T) {
	ring := NewRingAudioChannel(2)
	if !ring.Write([]float32{1}) || !ring.Write([]float32{2}) {
		t.Fatalf("initial writes failed")
	}
	if !ring.Write([]float32{3}) {
		t.Fatalf("overwrite write failed")
	}
	frame, ok := ring.Read()
	if !ok {
		t.Fatalf("expected frame after overwrite")
	}
	if frame[0] != 2 {
		t.Fatalf("expected first frame to be 2, got %v", frame)
	}
	frame, ok = ring.Read()
	if !ok || frame[0] != 3 {
		t.Fatalf("expected second frame to be 3, got %v", frame)
	}
	_, _, dropped, _ := ring.Stats()
	if dropped != 1 {
		t.Fatalf("expected 1 dropped frame, got %d", dropped)
	}
}

func TestRingAudioChannel_Concurrent(t *testing.T) {
	ring := NewRingAudioChannel(64)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	producer := func() {
		defer wg.Done()
		frame := []float32{1, 2, 3}
		for i := 0; i < 1_000; i++ {
			select {
			case <-ctx.Done():
				return
			default:
			}
			ring.Write(frame)
		}
	}
	consumer := func() {
		defer wg.Done()
		for {
			frame, ok := ring.ReadContext(ctx)
			if !ok {
				return
			}
			if len(frame) != 3 {
				select {
				case errCh <- fmt.Errorf("unexpected frame length %d", len(frame)):
				default:
				}
				return
			}
		}
	}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go producer()
	}
	wg.Add(1)
	go consumer()
	time.Sleep(10 * time.Millisecond)
	cancel()
	ring.Close()
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatalf("consumer error: %v", err)
	default:
	}
}

func TestRingAudioChannel_CloseWhileReading(t *testing.T) {
	ring := NewRingAudioChannel(4)
	done := make(chan error, 1)
	go func() {
		_, ok := ring.Read()
		if ok {
			done <- fmt.Errorf("expected read to fail after close")
			return
		}
		done <- nil
	}()
	time.Sleep(5 * time.Millisecond)
	ring.Close()
	if err := <-done; err != nil {
		t.Fatalf("read goroutine error: %v", err)
	}
}

func TestRingAudioChannel_TryRead(t *testing.T) {
	ring := NewRingAudioChannel(2)
	_, ok, empty := ring.TryRead()
	if ok || !empty {
		t.Fatalf("expected empty result")
	}
	ring.Write([]float32{1})
	frame, ok, empty := ring.TryRead()
	if !ok || empty || frame[0] != 1 {
		t.Fatalf("unexpected try read output: ok=%v empty=%v frame=%v", ok, empty, frame)
	}
	ring.Close()
	_, ok, empty = ring.TryRead()
	if ok || empty {
		t.Fatalf("expected closed state")
	}
}

func BenchmarkRingAudioChannel_Write(b *testing.B) {
	ring := NewRingAudioChannel(256)
	frame := make([]float32, 160)
	for i := 0; i < b.N; i++ {
		ring.Write(frame)
		ring.TryRead()
	}
}

func BenchmarkRingAudioChannel_Read(b *testing.B) {
	ring := NewRingAudioChannel(256)
	frame := make([]float32, 160)
	go func() {
		for i := 0; i < b.N; i++ {
			ring.Write(frame)
		}
		ring.Close()
	}()
	for {
		if _, ok := ring.Read(); !ok {
			return
		}
	}
}
