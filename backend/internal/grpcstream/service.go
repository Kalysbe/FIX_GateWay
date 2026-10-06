package grpcstream

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Authorizer interface {
	AuthenticateSession(context.Context, string, string) (*auth.AdminClaims, error)
	ValidateSession(context.Context, string, string, int64) error
}
type Service struct {
	pb.UnimplementedRealtimeServiceServer
	Hub       *realtime.Hub
	Users     Authorizer
	JWTSecret string
	Admission *ratelimit.Admission
	Log       *slog.Logger
}

func (s *Service) admit(user, operation string) (func(), error) {
	if s.Admission == nil {
		return func() {}, nil
	}
	release, denied := s.Admission.Acquire("admin", user, "System", operation)
	if denied == nil {
		return release, nil
	}
	if denied.Sample && s.Log != nil {
		s.Log.Warn("RATE_LIMIT_EXCEEDED", "scope", "admin", "user_id", user, "method", operation, "limit", denied.Policy.Rate, "burst", denied.Policy.Burst, "current_rate", denied.CurrentRate, "reason", denied.Reason)
	}
	st := status.New(codes.ResourceExhausted, denied.Error())
	detailed, err := st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(denied.RetryAfter)})
	if err != nil {
		return nil, st.Err()
	}
	return nil, detailed.Err()
}

func (s *Service) Stream(stream grpc.BidiStreamingServer[pb.ClientMessage, pb.ServerMessage]) error {
	ctx := stream.Context()
	if s.Users == nil || s.Hub == nil {
		return status.Error(codes.Unavailable, "realtime unavailable")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return status.Error(codes.Unauthenticated, "bearer token required")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	claims, err := s.Users.AuthenticateSession(checkCtx, s.JWTSecret, strings.TrimPrefix(values[0], "Bearer "))
	cancel()
	if err != nil {
		return status.Error(codes.Unauthenticated, "invalid session")
	}
	releaseConnect, err := s.admit(claims.UserID, "Connect")
	if err != nil {
		return err
	}
	defer releaseConnect()
	// One receive goroutine, one send path. Returning cancels outstanding gRPC IO.
	incoming := make(chan *pb.ClientMessage)
	recvErr := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			m, err := stream.Recv()
			if err != nil {
				select {
				case recvErr <- err:
				case <-done:
				}
				return
			}
			select {
			case incoming <- m:
			case <-done:
				return
			}
		}
	}()
	initial := time.NewTimer(5 * time.Second)
	defer initial.Stop()
	var first *pb.ClientMessage
	select {
	case first = <-incoming:
	case <-initial.C:
		return status.Error(codes.DeadlineExceeded, "subscribe required")
	case <-recvErr:
		return status.Error(codes.InvalidArgument, "subscribe required")
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	}
	if first.GetSubscribe() == nil {
		return status.Error(codes.InvalidArgument, "first message must subscribe")
	}
	releaseSubscribe, err := s.admit(claims.UserID, "Subscribe")
	if err != nil {
		return err
	}
	sub, err := s.Hub.Subscribe(claims.RoleCode, first.GetSubscribe().GetEventTypes())
	releaseSubscribe()
	if err == realtime.ErrPermission {
		return status.Error(codes.PermissionDenied, "subscription not permitted")
	}
	if err != nil {
		return status.Error(codes.ResourceExhausted, "stream capacity exceeded")
	}
	defer sub.Close()
	send := func(m *pb.ServerMessage) error {
		result := make(chan error, 1)
		go func() { result <- stream.Send(m) }()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case err := <-result:
			return err
		case <-timer.C:
			return status.Error(codes.DeadlineExceeded, "slow stream consumer")
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
	}
	ready := func(types []string) error {
		return send(&pb.ServerMessage{Response: &pb.ServerMessage_Ready{Ready: &pb.Ready{EventTypes: types}}})
	}
	if err := ready(first.GetSubscribe().GetEventTypes()); err != nil {
		return err
	}
	validate := time.NewTicker(5 * time.Second)
	defer validate.Stop()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	expiry := time.NewTimer(time.Until(claims.ExpiresAt.Time))
	defer expiry.Stop()
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case <-expiry.C:
			return status.Error(codes.Unauthenticated, "session expired")
		case <-validate.C:
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := s.Users.ValidateSession(checkCtx, claims.UserID, claims.RoleCode, claims.UserVersion)
			cancel()
			if err != nil {
				return status.Error(codes.Unauthenticated, "session revoked")
			}
		case <-sub.Done:
			return status.Error(codes.ResourceExhausted, "consumer queue full")
		case err := <-recvErr:
			if err == io.EOF {
				recvErr = nil
				continue
			}
			return err
		case m := <-incoming:
			operation := "Ping"
			if m.GetSubscribe() != nil {
				operation = "Subscribe"
			}
			releaseCommand, err := s.admit(claims.UserID, operation)
			if err != nil {
				return err
			}
			commandErr := func() error {
				defer releaseCommand()
				switch c := m.Command.(type) {
				case *pb.ClientMessage_Subscribe:
					if err := sub.SetTopics(c.Subscribe.GetEventTypes()); err != nil {
						return status.Error(codes.PermissionDenied, "subscription not permitted")
					}
					if err := ready(c.Subscribe.GetEventTypes()); err != nil {
						return err
					}
				case *pb.ClientMessage_Ping:
					if len(c.Ping.GetId()) > 128 {
						return status.Error(codes.InvalidArgument, "ping id too long")
					}
					if err := send(&pb.ServerMessage{Response: &pb.ServerMessage_Pong{Pong: &pb.Pong{Id: c.Ping.GetId()}}}); err != nil {
						return err
					}
				default:
					return status.Error(codes.InvalidArgument, "unknown stream command")
				}
				return nil
			}()
			if commandErr != nil {
				return commandErr
			}
		case event := <-sub.Events:
			if !time.Now().Before(claims.ExpiresAt.Time) {
				return status.Error(codes.Unauthenticated, "session expired")
			}
			if err := send(&pb.ServerMessage{Response: &pb.ServerMessage_Event{Event: event}}); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := send(&pb.ServerMessage{Response: &pb.ServerMessage_Pong{Pong: &pb.Pong{}}}); err != nil {
				return err
			}
		}
	}
}
