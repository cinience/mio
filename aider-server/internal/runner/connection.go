package runner

import (
	"context"
	"errors"

	"aider-server/internal/runnerpb"
)

type connection struct {
	id     string
	stream runnerpb.RunnerControl_ConnectServer
	sendCh chan *runnerpb.ServerMessage
	done   chan struct{}
}

func newConnection(id string, stream runnerpb.RunnerControl_ConnectServer) *connection {
	return &connection{
		id:     id,
		stream: stream,
		sendCh: make(chan *runnerpb.ServerMessage, 64),
		done:   make(chan struct{}),
	}
}

func (c *connection) send(ctx context.Context, msg *runnerpb.ServerMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("runner connection closed")
	case c.sendCh <- msg:
		return nil
	}
}

func (c *connection) runSendLoop() {
	defer close(c.done)
	for msg := range c.sendCh {
		if err := c.stream.Send(msg); err != nil {
			return
		}
	}
}

func (c *connection) close() {
	close(c.sendCh)
	<-c.done
}
