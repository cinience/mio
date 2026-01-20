package task

import (
	"fmt"
	"time"
)

func WaitAndSendEnter(lastActivity func() time.Time, send func([]byte) error, timeout time.Duration, quiet time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		last := lastActivity()
		if last.IsZero() || time.Since(last) >= quiet {
			if err := send([]byte("\r")); err != nil {
				return err
			}
			time.Sleep(50 * time.Millisecond)
			return send([]byte("\n"))
		}
		if time.Now().After(deadline) {
			if err := send([]byte("\r")); err != nil {
				return fmt.Errorf("enter timeout after %s: %w", timeout, err)
			}
			time.Sleep(50 * time.Millisecond)
			if err := send([]byte("\n")); err != nil {
				return fmt.Errorf("enter timeout after %s: %w", timeout, err)
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}
