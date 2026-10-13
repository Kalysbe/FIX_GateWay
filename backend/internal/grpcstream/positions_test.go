package grpcstream

import (
	"context"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func TestBrokerPositionSnapshotsOverGRPC(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	defer lis.Close()
	hub := realtime.NewHub()
	var version atomic.Int64
	svc := &Service{Hub: hub, Brokers: &testBrokers{tokenID: uuid.New()}, PositionsSnapshot: func(ctx context.Context, firms []string) (any, error) {
		if !reflect.DeepEqual(firms, []string{"TEST", ""}) {
			t.Errorf("untrusted firm scope: %v", firms)
		}
		return map[string]any{"firm_id": "TEST", "cash": []any{map[string]any{"pos_amt": version.Load()}}, "instruments": []any{}}, nil
	}}
	server := grpc.NewServer()
	defer server.Stop()
	pb.RegisterRealtimeServiceServer(server, svc)
	go server.Serve(lis)
	conn, err := grpc.NewClient("passthrough:///positions", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer broker-token", "x-firm-id", "OTHER")
	stream, err := pb.NewRealtimeServiceClient(conn).Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{EventTypes: []string{"positions.updated"}}}}); err != nil {
		t.Fatal(err)
	}
	if msg, err := stream.Recv(); err != nil || msg.GetReady() == nil {
		t.Fatal("missing ready", msg, err)
	}
	initial, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if initial.GetEvent().GetType() != "positions.updated" || initial.GetEvent().GetData().Fields["firm_id"].GetStringValue() != "TEST" {
		t.Fatal(initial)
	}
	version.Store(12)
	hub.Broadcast("positions.updated", map[string]any{"firm_id": "OTHER", "secret": "hidden"})
	updated, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	fields := updated.GetEvent().GetData().Fields
	if fields["secret"] != nil || fields["cash"].GetListValue().Values[0].GetStructValue().Fields["pos_amt"].GetNumberValue() != 12 {
		t.Fatal("incorrect scoped update", updated)
	}
	if updated.GetEvent().Sequence <= initial.GetEvent().Sequence {
		t.Fatal("sequence not increasing")
	}
}
