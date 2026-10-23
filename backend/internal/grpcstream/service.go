package grpcstream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"encoding/json"
	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

type Authorizer interface {
	AuthenticateSession(context.Context, string, string) (*auth.AdminClaims, error)
	ValidateSession(context.Context, string, string, int64) error
}
type Service struct {
	pb.UnimplementedRealtimeServiceServer
	Hub               *realtime.Hub
	Users             Authorizer
	JWTSecret         string
	Admission         *ratelimit.Admission
	Log               *slog.Logger
	Brokers           BrokerAuthorizer
	HMACRequired      bool
	Nonces            auth.NonceStore
	PositionsSnapshot func(context.Context, []string) (any, error)
}

func (s *Service) admit(user, operation string) (func(), error) {
	if s.Admission == nil {
		return func() {}, nil
	}
	scope := "admin"
	if strings.HasPrefix(user, "broker:") {
		scope = "broker"
		user = strings.TrimPrefix(user, "broker:")
	}
	release, denied := s.Admission.Acquire(scope, user, "System", operation)
	if denied == nil {
		return release, nil
	}
	if denied.Sample && s.Log != nil {
		s.Log.Warn("RATE_LIMIT_EXCEEDED", "scope", scope, "subject", user, "method", operation, "limit", denied.Policy.Rate, "burst", denied.Policy.Burst, "current_rate", denied.CurrentRate, "reason", denied.Reason)
	}
	st := status.New(codes.ResourceExhausted, denied.Error())
	detailed, err := st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(denied.RetryAfter)})
	if err != nil {
		return nil, st.Err()
	}
	return nil, detailed.Err()
}

const orderRevalidateEvery = time.Second

func (s *Service) Stream(stream grpc.BidiStreamingServer[pb.ClientMessage, pb.ServerMessage]) error {
	ctx := stream.Context()
	if s.Hub == nil {
		return status.Error(codes.Unavailable, "realtime unavailable")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return status.Error(codes.Unauthenticated, "bearer token required")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	principal, err := s.authenticate(checkCtx, md, strings.TrimPrefix(values[0], "Bearer "))
	cancel()
	if err != nil {
		if errors.Is(err, auth.ErrNonceUnavailable) || errors.Is(err, brokers.ErrAuthenticationUnavailable) || errors.Is(err, admin.ErrAuthenticationUnavailable) {
			return status.Error(codes.Unavailable, "authentication temporarily unavailable")
		}
		return status.Error(codes.Unauthenticated, "invalid session")
	}
	releaseConnect, err := s.admit(principal.id, "Connect")
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
	releaseSubscribe, err := s.admit(principal.id, "Subscribe")
	if err != nil {
		return err
	}
	var sub *realtime.Subscription
	if principal.info.BrokerID > 0 {
		sub, err = s.Hub.SubscribeBroker(first.GetSubscribe().GetEventTypes(), principal.info, principal.permissions)
	} else {
		sub, err = s.Hub.SubscribeIdentified(principal.role, first.GetSubscribe().GetEventTypes(), principal.info)
	}
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
	positionEvent := func(event *pb.Event) error {
		if s.PositionsSnapshot == nil {
			return status.Error(codes.Unavailable, "positions unavailable")
		}
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		// Revalidate before reading so changed firm scope cannot receive data.
		if err := principal.validate(check); err != nil {
			if errors.Is(err, admin.ErrAuthenticationUnavailable) || errors.Is(err, brokers.ErrAuthenticationUnavailable) {
				return status.Error(codes.Unavailable, "authentication temporarily unavailable")
			}
			return status.Error(codes.Unauthenticated, "session revoked")
		}
		data, err := s.PositionsSnapshot(check, principal.firms)
		if err != nil {
			return status.Error(codes.Unavailable, "positions unavailable")
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			return status.Error(codes.Internal, "positions encoding failed")
		}
		payload := new(structpb.Struct)
		if err := protojson.Unmarshal(encoded, payload); err != nil {
			return status.Error(codes.Internal, "positions encoding failed")
		}
		copy := &pb.Event{Sequence: event.Sequence, Type: "positions.updated", Data: payload, OccurredAt: event.OccurredAt}
		return send(&pb.ServerMessage{Response: &pb.ServerMessage_Event{Event: copy}})
	}
	if err := ready(sub.Topics()); err != nil {
		return err
	}
	validate := time.NewTicker(5 * time.Second)
	defer validate.Stop()
	// order.updated is firm-scoped, so the token is rechecked before delivery,
	// but at most once per orderRevalidateEvery: a burst of Orders changes must
	// not turn into one database lookup per event per stream.
	validatedAt := time.Now()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	expiry := time.NewTimer(time.Until(principal.expires))
	defer expiry.Stop()
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case <-expiry.C:
			return status.Error(codes.Unauthenticated, "session expired")
		case <-validate.C:
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := principal.validate(checkCtx)
			cancel()
			if err != nil {
				if errors.Is(err, admin.ErrAuthenticationUnavailable) || errors.Is(err, brokers.ErrAuthenticationUnavailable) {
					return status.Error(codes.Unavailable, "authentication temporarily unavailable")
				}
				return status.Error(codes.Unauthenticated, "session revoked")
			}
			validatedAt = time.Now()
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
			releaseCommand, err := s.admit(principal.id, operation)
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
					if err := ready(sub.Topics()); err != nil {
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
			if !time.Now().Before(principal.expires) {
				return status.Error(codes.Unauthenticated, "session expired")
			}
			if principal.info.BrokerID > 0 && event.Type == "order.updated" && time.Since(validatedAt) >= orderRevalidateEvery {
				check, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := principal.validate(check)
				cancel()
				if err != nil {
					if errors.Is(err, brokers.ErrAuthenticationUnavailable) {
						return status.Error(codes.Unavailable, "authentication temporarily unavailable")
					}
					return status.Error(codes.Unauthenticated, "session revoked")
				}
				validatedAt = time.Now()
			}
			if principal.info.BrokerID > 0 && event.Type == "positions.updated" {
				if err := positionEvent(event); err != nil {
					return err
				}
				continue
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
