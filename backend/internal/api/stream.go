package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Server) SetRealtimeClient(client pb.RealtimeServiceClient) { s.realtimeClient = client }

// Browsers receive SSE from the same authenticated native gRPC stream. No WS or
// query credentials, and no duplicate subscription/authentication implementation.
func (s *Server) streamAdminEvents(w http.ResponseWriter, r *http.Request) {
	if s.realtimeClient == nil {
		writeErr(w, 503, "gRPC realtime unavailable")
		return
	}
	base, cancel := context.WithCancel(r.Context())
	defer cancel()
	ctx := metadata.AppendToOutgoingContext(base, "authorization", "Bearer "+bearer(r), "x-browser-bridge", "true")
	stream, err := s.realtimeClient.Stream(ctx)
	if err != nil {
		writeErr(w, 503, "gRPC realtime unavailable")
		return
	}
	defer stream.CloseSend()
	if err := stream.Send(&pb.ClientMessage{Command: &pb.ClientMessage_Subscribe{Subscribe: &pb.Subscribe{}}}); err != nil {
		writeErr(w, 503, "gRPC realtime unavailable")
		return
	}
	type initialResult struct {
		message *pb.ServerMessage
		err     error
	}
	initial := make(chan initialResult, 1)
	go func() { message, err := stream.Recv(); initial <- initialResult{message, err} }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var first *pb.ServerMessage
	select {
	case result := <-initial:
		first, err = result.message, result.err
	case <-timer.C:
		writeErr(w, 503, "gRPC realtime unavailable")
		return
	case <-r.Context().Done():
		return
	}
	if status.Code(err) == codes.Unauthenticated {
		writeErr(w, 401, "invalid session")
		return
	}
	if err != nil || first.GetReady() == nil {
		writeErr(w, 503, "gRPC realtime unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	write := func(payload string) error {
		if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		defer controller.SetWriteDeadline(time.Time{}) // Idle streams must survive between heartbeats.
		if _, err := fmt.Fprint(w, payload); err != nil {
			return err
		}
		return controller.Flush()
	}
	if write("event: ready\ndata: {}\n\n") != nil {
		return
	}
	for {
		message, err := stream.Recv()
		if err != nil {
			return
		}
		if event := message.GetEvent(); event != nil {
			data := []byte(event.GetDataJson())
			if len(data) == 0 {
				data, err = protojson.Marshal(event.GetData())
				if err != nil {
					return
				}
			} else if !json.Valid(data) {
				return
			}
			removed := ""
			if event.GetRemoved() {
				removed = ",\"removed\":true"
			}
			if err := write(fmt.Sprintf("id: %d\ndata: {\"type\":%q,\"data\":%s%s}\n\n", event.GetSequence(), event.GetType(), data, removed)); err != nil {
				return
			}
		} else if write(": heartbeat\n\n") != nil {
			return
		}
	}
}
