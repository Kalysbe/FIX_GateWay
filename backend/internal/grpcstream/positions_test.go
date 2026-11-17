package grpcstream

import (
	"context"
	"net"
	"reflect"
	"strconv"
	"strings"
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

// Money (MoneyLimits) and securities (DepoLimits) are separate events, each a
// full array of the token firm's rows of its table.
func TestBrokerPositionSnapshotsOverGRPC(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	defer lis.Close()
	hub := realtime.NewHub()
	var version atomic.Int64
	svc := &Service{Hub: hub, Brokers: &testBrokers{tokenID: uuid.New()}, PositionsSnapshot: func(ctx context.Context, table string, firms []string) (any, error) {
		if !reflect.DeepEqual(firms, []string{"TEST"}) {
			t.Errorf("untrusted firm scope: %v", firms)
		}
		return []any{map[string]any{"Table": table, "CurrentBal": version.Load()}}, nil
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
	if err := stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{EventTypes: []string{"moneylimits.updated", "depolimits.updated"}}}}); err != nil {
		t.Fatal(err)
	}
	if msg, err := stream.Recv(); err != nil || msg.GetReady() == nil {
		t.Fatal("missing ready", msg, err)
	}
	expect := func(eventType, table string, balance int64) *pb.Event {
		t.Helper()
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		event := msg.GetEvent()
		want := `[{"CurrentBal":` + strconv.FormatInt(balance, 10) + `,"Table":"` + table + `"}]`
		if event.GetType() != eventType || event.GetDataJson() != want || event.GetData() != nil {
			t.Fatalf("got %s %s, want %s %s", event.GetType(), event.GetDataJson(), eventType, want)
		}
		return event
	}
	expect("moneylimits.updated", "moneylimits", 0)
	initial := expect("depolimits.updated", "depolimits", 0)
	version.Store(12)
	hub.Broadcast("depolimits.updated", map[string]any{"firm_id": "OTHER", "secret": "hidden"})
	updated := expect("depolimits.updated", "depolimits", 12)
	if strings.Contains(updated.GetDataJson(), "secret") {
		t.Fatal("trigger data leaked", updated)
	}
	if updated.Sequence <= initial.Sequence {
		t.Fatal("sequence not increasing")
	}
}
