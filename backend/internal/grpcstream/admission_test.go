package grpcstream

import (
	"context"
	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"testing"
	"time"
)

func TestStreamSharedAdmissionDynamicConfiguration(t *testing.T) {
	a := ratelimit.NewAdmission()
	p := ratelimit.Policy{Scope: "admin", Subject: "*", Operation: "Ping", Rate: 1, Burst: 1, Concurrent: 2, Enabled: true}
	a.Replace([]ratelimit.Policy{p})
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterRealtimeServiceServer(server, &Service{Hub: realtime.NewHub(), Users: &testUsers{}, JWTSecret: "test", Admission: a})
	go server.Serve(listener)
	defer server.Stop()
	conn, err := grpc.NewClient("passthrough:///test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	token, err := auth.IssueAdminJWT("test", uuid.New(), "test@local", auth.RoleReadOnly, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token), 5*time.Second)
	defer cancel()
	newStream := func() grpc.BidiStreamingClient[pb.ClientMessage, pb.ServerMessage] {
		stream, err := pb.NewRealtimeServiceClient(conn).Stream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{}}}); err != nil {
			t.Fatal(err)
		}
		if _, err = stream.Recv(); err != nil {
			t.Fatal(err)
		}
		return stream
	}
	first, second := newStream(), newStream()
	ping := &pb.ClientMessage{Command: &pb.ClientMessage_Ping{Ping: &pb.Ping{Id: "test"}}}
	if err = first.Send(ping); err != nil {
		t.Fatal(err)
	}
	if _, err = first.Recv(); err != nil {
		t.Fatal(err)
	}
	if err = second.Send(ping); err != nil {
		t.Fatal(err)
	}
	_, err = second.Recv()
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatal("shared identity", err)
	}
	details := status.Convert(err).Details()
	if len(details) != 1 {
		t.Fatal("retry missing", details)
	}
	if _, ok := details[0].(*errdetails.RetryInfo); !ok {
		t.Fatal(details)
	}
	p.Enabled = false
	a.Replace([]ratelimit.Policy{p})
	if err = first.Send(ping); err != nil {
		t.Fatal(err)
	}
	if _, err = first.Recv(); err != nil {
		t.Fatal("active stream update", err)
	}
}
