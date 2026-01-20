package runner

import (
	"context"
	"fmt"
	"strings"

	"aider-server/internal/runnerpb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Bridge struct {
	runnerpb.UnimplementedRunnerControlServer
	manager *Manager
}

func NewBridge(manager *Manager) *Bridge {
	return &Bridge{manager: manager}
}

func (b *Bridge) Connect(stream runnerpb.RunnerControl_ConnectServer) error {
	if b.manager == nil {
		return status.Error(codes.Unavailable, "runner manager not configured")
	}
	if err := authorizeRunner(stream.Context(), b.manager); err != nil {
		return err
	}
	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := msg.GetHello()
	if hello == nil || hello.RunnerId == "" {
		return status.Error(codes.InvalidArgument, "runner hello required")
	}
	conn := b.manager.RegisterRunner(hello.RunnerId, stream)
	defer b.manager.UnregisterRunner(hello.RunnerId)

	ctx := stream.Context()
	b.manager.HandleHello(ctx, hello.RunnerId, hello)

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		if msg.GetHello() != nil {
			b.manager.HandleHello(ctx, hello.RunnerId, msg.GetHello())
			continue
		}
		b.manager.HandleRunnerMessage(context.Background(), conn.id, msg)
	}
}

func (b *Bridge) String() string {
	if b.manager == nil {
		return "runner bridge (unconfigured)"
	}
	return fmt.Sprintf("runner bridge (%p)", b.manager)
}

func authorizeRunner(ctx context.Context, manager *Manager) error {
	if manager == nil || strings.TrimSpace(manager.authToken) == "" {
		return nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing runner authorization")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "missing runner authorization")
	}
	token := strings.TrimSpace(values[0])
	if !strings.HasPrefix(token, "Bearer ") {
		return status.Error(codes.Unauthenticated, "invalid runner authorization")
	}
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" || token != manager.authToken {
		return status.Error(codes.Unauthenticated, "invalid runner token")
	}
	return nil
}
