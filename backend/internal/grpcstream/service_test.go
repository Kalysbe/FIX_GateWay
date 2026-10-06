package grpcstream

import (
	"context"
	"errors"
	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/platform/devcert"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type testUsers struct{ revoked atomic.Bool }

func (u *testUsers) AuthenticateSession(ctx context.Context, secret, token string) (*auth.AdminClaims, error) {
	c, e := auth.ParseAdminJWT(secret, token)
	if e != nil {
		return nil, e
	}
	return c, u.ValidateSession(ctx, c.UserID, c.RoleCode, c.UserVersion)
}
func (u *testUsers) ValidateSession(context.Context, string, string, int64) error {
	if u.revoked.Load() {
		return errors.New("revoked")
	}
	return nil
}

func TestMutualTLSStreaming(t *testing.T) {
	dir := t.TempDir()
	if err := devcert.Generate(dir, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	file := func(n string) string { return filepath.Join(dir, n) }
	creds, err := ServerCredentials(file("server.crt"), file("server.key"), file("ca.crt"), true)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.NewHub()
	users := &testUsers{}
	server := grpc.NewServer(grpc.Creds(creds), grpc.MaxRecvMsgSize(16*1024))
	pb.RegisterRealtimeServiceServer(server, &Service{Hub: hub, Users: users, JWTSecret: "test-secret"})
	go server.Serve(listener)
	defer server.Stop()
	client := func(cert, key, ca, name string) pb.RealtimeServiceClient {
		t.Helper()
		creds, e := ClientCredentials(cert, key, ca, name)
		if e != nil {
			t.Fatal(e)
		}
		conn, e := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(creds))
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { conn.Close() })
		return pb.NewRealtimeServiceClient(conn)
	}
	good := client(file("bridge.crt"), file("bridge.key"), file("ca.crt"), "localhost")
	token, err := auth.IssueAdminJWT("test-secret", uuid.New(), "ro@test", auth.RoleReadOnly, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	open := func(c pb.RealtimeServiceClient, token string, types []string) (grpc.BidiStreamingClient[pb.ClientMessage, pb.ServerMessage], context.CancelFunc) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
		stream, e := c.Stream(ctx)
		if e != nil {
			cancel()
			t.Fatal(e)
		}
		_ = stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{EventTypes: types}}})
		return stream, cancel
	}
	t.Run("bidirectional_role_filter_and_half_close", func(t *testing.T) {
		stream, cancel := open(good, token, nil)
		defer cancel()
		ready, e := stream.Recv()
		if e != nil || ready.GetReady() == nil {
			t.Fatalf("ready: %v %v", ready, e)
		}
		if e := stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Ping{Ping: &pb.Ping{Id: "ping-1"}}}); e != nil {
			t.Fatal(e)
		}
		pong, e := stream.Recv()
		if e != nil || pong.GetPong().GetId() != "ping-1" {
			t.Fatalf("pong: %v %v", pong, e)
		}
		if e := stream.CloseSend(); e != nil {
			t.Fatal(e)
		}
		hub.Broadcast("obs.event", map[string]any{"private": true})
		hub.Broadcast("marketdata.tick", map[string]any{"price": "123456.789012345"})
		event, e := stream.Recv()
		if e != nil || event.GetEvent().GetType() != "marketdata.tick" || event.GetEvent().GetData().GetFields()["price"].GetStringValue() != "123456.789012345" {
			t.Fatalf("event: %v %v", event, e)
		}
	})
	t.Run("forbidden_topic", func(t *testing.T) {
		stream, cancel := open(good, token, []string{"obs.event"})
		defer cancel()
		_, e := stream.Recv()
		if status.Code(e) != codes.PermissionDenied {
			t.Fatal(e)
		}
	})
	t.Run("invalid_token", func(t *testing.T) {
		stream, cancel := open(good, "invalid", nil)
		defer cancel()
		_, e := stream.Recv()
		if status.Code(e) != codes.Unauthenticated {
			t.Fatal(e)
		}
	})
	other := t.TempDir()
	if e := devcert.Generate(other, []string{"localhost"}); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ name, cert, key, ca, hostname string }{
		{"missing_client_certificate", "", "", file("ca.crt"), "localhost"},
		{"untrusted_client", filepath.Join(other, "bridge.crt"), filepath.Join(other, "bridge.key"), file("ca.crt"), "localhost"},
		{"untrusted_server", file("bridge.crt"), file("bridge.key"), filepath.Join(other, "ca.crt"), "localhost"},
		{"wrong_hostname", file("bridge.crt"), file("bridge.key"), file("ca.crt"), "wrong.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := client(tc.cert, tc.key, tc.ca, tc.hostname)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
			stream, e := c.Stream(ctx)
			if e == nil {
				_ = stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{}}})
				_, e = stream.Recv()
			}
			if e == nil {
				t.Fatal("untrusted connection accepted")
			}
		})
	}
	t.Run("active_session_revoked", func(t *testing.T) {
		stream, cancel := open(good, token, nil)
		defer cancel()
		if _, e := stream.Recv(); e != nil {
			t.Fatal(e)
		}
		users.revoked.Store(true)
		defer users.revoked.Store(false)
		_, e := stream.Recv()
		if status.Code(e) != codes.Unauthenticated {
			t.Fatal(e)
		}
	})
}
