package grpcstream

import (
	"context"
	"fmt"
	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type testBrokers struct {
	revoked atomic.Bool
	tokenID uuid.UUID
}

func (b *testBrokers) AuthenticateToken(_ context.Context, raw, ip string) (domain.Broker, domain.APIToken, error) {
	if raw != "broker-token" || b.revoked.Load() {
		return domain.Broker{}, domain.APIToken{}, fmt.Errorf("invalid token")
	}
	return domain.Broker{ID: 7, Code: "TEST", Name: "Test broker"}, domain.APIToken{ID: b.tokenID, BrokerID: 7, Permissions: auth.ReadOnlyPermissions}, nil
}
func TestBrokerGRPCSubscribeAndRevocation(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	defer listener.Close()
	hub := realtime.NewHub()
	brokers := &testBrokers{tokenID: uuid.New()}
	server := grpc.NewServer()
	defer server.Stop()
	pb.RegisterRealtimeServiceServer(server, &Service{Hub: hub, Brokers: brokers, HMACRequired: true, Nonces: auth.NewNonceCache(time.Minute)})
	go server.Serve(listener)
	conn, err := grpc.NewClient("passthrough:///test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewRealtimeServiceClient(conn)
	ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), uuid.NewString()
	sig := auth.SignHMACSHA256("broker-token", auth.BuildHMACPayload(ts, nonce, "POST", streamPath, ""))
	open := func(signature, nonce string, topics []string) grpc.BidiStreamingClient[pb.ClientMessage, pb.ServerMessage] {
		ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
		t.Cleanup(cancel)
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer broker-token", "x-timestamp", ts, "x-nonce", nonce, "x-signature", signature)
		stream, err := client.Stream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{EventTypes: topics}}}); err != nil {
			t.Fatal(err)
		}
		return stream
	}
	denied := open("bad", uuid.NewString(), []string{"order.updated"})
	if _, err := denied.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	stream := open(sig, nonce, []string{"order.updated"})
	if ready, err := stream.Recv(); err != nil || ready.GetReady() == nil {
		t.Fatalf("ready: %v %v", ready, err)
	}
	if connections := hub.Connections().Connections; len(connections) != 1 || connections[0].BrokerID != 7 {
		t.Fatal(connections)
	}
	replay := open(sig, nonce, []string{"order.updated"})
	if _, err := replay.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatal("nonce replay accepted", err)
	}
	hub.BroadcastForBroker("order.updated", map[string]any{"broker_id": 8}, 8)
	hub.BroadcastForBroker("order.updated", map[string]any{"broker_id": 7}, 7)
	event, err := stream.Recv()
	if err != nil || event.GetEvent().GetData().Fields["broker_id"].GetNumberValue() != 7 {
		t.Fatal("broker isolation", event, err)
	}
	brokers.revoked.Store(true)
	if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatal("revocation", err)
	}
	deadline := time.Now().Add(time.Second)
	for len(hub.Connections().Connections) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(hub.Connections().Connections) != 0 {
		t.Fatal("revoked subscription retained")
	}
}
