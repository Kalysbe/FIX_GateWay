package grpcstream

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

func TestBrokerTokenAutomaticRouting(t *testing.T) {
	s := &Service{Brokers: &testBrokers{tokenID: uuid.New()}}
	for _, tc := range []struct {
		name    string
		md      metadata.MD
		raw     string
		allowed bool
	}{
		{"bearer only", metadata.MD{}, "broker-token", true},
		{"explicit broker", metadata.Pairs("x-client-type", "broker"), "broker-token", true},
		{"explicit admin", metadata.Pairs("x-client-type", "admin"), "broker-token", false},
		{"invalid token", metadata.MD{}, "invalid-token", false},
		{"JWT stays admin", metadata.MD{}, "header.payload.signature", false},
		{"invalid client type", metadata.Pairs("x-client-type", "other"), "broker-token", false},
		{"duplicate client type", metadata.Pairs("x-client-type", "broker", "x-client-type", "broker"), "broker-token", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := s.authenticate(context.Background(), tc.md, tc.raw)
			if !tc.allowed {
				if err == nil {
					t.Fatal("unauthorized token accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.role != "BROKER" || p.info.BrokerID != 7 {
				t.Fatalf("incorrect broker identity: %+v", p.info)
			}
		})
	}
	// Automatic routing must not bypass mandatory HMAC.
	s.HMACRequired = true
	if _, err := s.authenticate(context.Background(), metadata.MD{}, "broker-token"); err == nil {
		t.Fatal("unsigned broker accepted with mandatory HMAC")
	}
}
