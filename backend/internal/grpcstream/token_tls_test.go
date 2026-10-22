package grpcstream

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/platform/devcert"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestBrokerTokenOverTLSWithoutClientCertificate(t *testing.T) {
	dir := t.TempDir()
	if err := devcert.Generate(dir, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	file := func(name string) string { return filepath.Join(dir, name) }
	serverCredentials, err := ServerCredentials(file("server.crt"), file("server.key"), "", false)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.NewHub()
	brokers := &testBrokers{tokenID: uuid.New()}
	server := grpc.NewServer(grpc.Creds(serverCredentials))
	pb.RegisterRealtimeServiceServer(server, &Service{Hub: hub, Brokers: brokers})
	go server.Serve(listener)
	t.Cleanup(server.Stop)

	clientCredentials, err := ClientCredentials("", "", file("ca.crt"), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(clientCredentials))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := pb.NewRealtimeServiceClient(conn)
	open := func(t *testing.T, header string, topics []string) grpc.BidiStreamingClient[pb.ClientMessage, pb.ServerMessage] {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
		t.Cleanup(cancel)
		if header != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", header)
		}
		stream, err := client.Stream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Authentication errors may reach the client before Send completes.
		_ = stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{EventTypes: topics}}})
		return stream
	}

	for _, header := range []string{"", "Bearer invalid-token"} {
		t.Run("authentication_"+header, func(t *testing.T) {
			if _, err := open(t, header, []string{"order.updated"}).Recv(); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("accepted unauthenticated TLS connection: %v", err)
			}
		})
	}
	t.Run("permissions", func(t *testing.T) {
		if _, err := open(t, "Bearer broker-token", []string{"obs.event"}).Recv(); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("accepted administrative topic: %v", err)
		}
	})
	t.Run("isolation_and_revocation", func(t *testing.T) {
		stream := open(t, "Bearer broker-token", []string{"order.updated"})
		ready, err := stream.Recv()
		if err != nil || ready.GetReady() == nil {
			t.Fatalf("Bearer-only TLS stream failed: %v %v", ready, err)
		}
		hub.BroadcastForBroker("order.updated", map[string]any{"broker_id": 8}, 8)
		hub.BroadcastForBroker("order.updated", map[string]any{"broker_id": 7}, 7)
		event, err := stream.Recv()
		if err != nil || event.GetEvent().GetData().Fields["broker_id"].GetNumberValue() != 7 {
			t.Fatalf("broker isolation failed: %v %v", event, err)
		}
		brokers.revoked.Store(true)
		defer brokers.revoked.Store(false)
		if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("revoked token retained its stream: %v", err)
		}
	})
	t.Run("plaintext_rejected", func(t *testing.T) {
		conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer broker-token")
		stream, err := pb.NewRealtimeServiceClient(conn).Stream(ctx)
		if err == nil {
			_ = stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{}}})
			_, err = stream.Recv()
		}
		if err == nil {
			t.Fatal("plaintext gRPC connection accepted")
		}
	})
}

func TestOptionalBrokerHMACStillValidatesSuppliedSignature(t *testing.T) {
	service := &Service{Brokers: &testBrokers{tokenID: uuid.New()}}
	for _, md := range []metadata.MD{
		metadata.Pairs("x-signature", "invalid"),
		metadata.Pairs("x-timestamp", "0", "x-nonce", "nonce-1234", "x-signature", "invalid"),
	} {
		if _, err := service.authenticate(context.Background(), md, "broker-token"); err == nil {
			t.Fatal("invalid optional HMAC accepted")
		}
	}
	service.HMACRequired = true
	service.Nonces = auth.NewNonceCache(time.Minute)
	if _, err := service.authenticate(context.Background(), metadata.MD{}, "broker-token"); err == nil {
		t.Fatal("required HMAC bypassed")
	}
}
