package api

import (
	"bufio"
	"context"
	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/grpcstream"
	"github.com/fix-gateway/backend/internal/platform/devcert"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type streamTestUsers struct{}

func (streamTestUsers) AuthenticateSession(_ context.Context, secret, token string) (*auth.AdminClaims, error) {
	return auth.ParseAdminJWT(secret, token)
}
func (streamTestUsers) ValidateSession(context.Context, string, string, int64) error { return nil }

func configureTestRealtime(t *testing.T, s *Server, users grpcstream.Authorizer, secret string) {
	t.Helper()
	dir := t.TempDir()
	if err := devcert.Generate(dir, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	file := func(n string) string { return filepath.Join(dir, n) }
	serverCreds, e := grpcstream.ServerCredentials(file("server.crt"), file("server.key"), file("ca.crt"), true)
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	native := grpc.NewServer(grpc.Creds(serverCreds))
	pb.RegisterRealtimeServiceServer(native, &grpcstream.Service{Hub: s.hub, Users: users, JWTSecret: secret})
	go native.Serve(listener)
	t.Cleanup(native.Stop)
	creds, e := grpcstream.ClientCredentials(file("bridge.crt"), file("bridge.key"), file("ca.crt"), "localhost")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(creds))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	s.SetRealtimeClient(pb.NewRealtimeServiceClient(conn))
}
func TestBrowserStreamUsesAuthenticatedMutualTLS(t *testing.T) {
	hub := realtime.NewHub()
	s := &Server{hub: hub}
	configureTestRealtime(t, s, streamTestUsers{}, "test-key")
	server := httptest.NewServer(http.HandlerFunc(s.streamAdminEvents))
	defer server.Close()
	token, e := auth.IssueAdminJWT("test-key", uuid.New(), "ro@test", auth.RoleReadOnly, time.Minute, 1)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/admin/events/stream", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(response.Status)
	}
	scanner := bufio.NewScanner(response.Body)
	if !scanner.Scan() || scanner.Text() != "event: ready" {
		t.Fatal("missing ready", scanner.Err())
	}
	hub.Broadcast("obs.event", map[string]any{"private": true})
	hub.Broadcast("marketdata.tick", map[string]any{"price": "12.34"})
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") && strings.Contains(line, "marketdata.tick") {
			if !strings.Contains(line, "12.34") {
				t.Fatal(line)
			}
			return
		}
		if strings.Contains(line, "private") {
			t.Fatal("private event leaked")
		}
	}
	t.Fatal("stream stopped", scanner.Err())
}
