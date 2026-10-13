package grpcstream

import (
	"context"
	"errors"
	"fmt"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/realtime"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"net"
	"slices"
	"strings"
	"time"
)

type BrokerAuthorizer interface {
	AuthenticateToken(context.Context, string, string) (domain.Broker, domain.APIToken, error)
}
type principal struct {
	id, role    string
	expires     time.Time
	info        realtime.ConnectionInfo
	permissions []string
	firms       []string
	validate    func(context.Context) error
}

const streamPath = "/fixgateway.realtime.v1.RealtimeService/Stream"

func (s *Service) authenticate(ctx context.Context, md metadata.MD, raw string) (principal, error) {
	p := principal{}
	remote := ""
	if peer, ok := peer.FromContext(ctx); ok && peer.Addr != nil {
		remote = peer.Addr.String()
	}
	p.info = realtime.ConnectionInfo{Peer: remote, Transport: "gRPC"}
	if values := md.Get("x-client-type"); len(values) > 1 {
		return p, fmt.Errorf("invalid client type")
	} else if len(values) == 1 && values[0] != "broker" && values[0] != "admin" {
		return p, fmt.Errorf("invalid client type")
	}
	// Issued API tokens use base64url without dots; admin sessions are JWTs.
	// An explicit client type always takes precedence over automatic routing.
	clientType := md.Get("x-client-type")
	if (len(clientType) == 1 && clientType[0] == "broker") || (len(clientType) == 0 && !strings.Contains(raw, ".")) {
		if s.Brokers == nil {
			return p, fmt.Errorf("broker realtime unavailable")
		}
		ip, _, err := net.SplitHostPort(remote)
		if err != nil {
			ip = remote
		}
		b, tok, err := s.Brokers.AuthenticateToken(ctx, raw, ip)
		if err != nil {
			return p, err
		}
		one := func(key string) string {
			v := md.Get(key)
			if len(v) == 1 {
				return v[0]
			}
			return ""
		}
		ts, nonce, sig := one("x-timestamp"), one("x-nonce"), one("x-signature")
		if s.HMACRequired || ts != "" || nonce != "" || sig != "" {
			if ts == "" || nonce == "" || sig == "" || auth.ValidateTimestamp(ts, 5*time.Minute) != nil || !auth.VerifyHMACSHA256(raw, auth.BuildHMACPayload(ts, nonce, "POST", streamPath, ""), sig) {
				return p, fmt.Errorf("invalid signature")
			}
			if s.Nonces == nil {
				return p, auth.ErrNonceUnavailable
			}
			if err := s.Nonces.UseScopedContext(ctx, auth.HashToken(raw, "hmac-nonce"), nonce); err != nil {
				if errors.Is(err, auth.ErrNonceUnavailable) {
					return p, err
				}
				return p, fmt.Errorf("invalid nonce")
			}
		}
		p.id = fmt.Sprintf("broker:%d", b.ID)
		p.role = "BROKER"
		p.permissions = append([]string(nil), tok.Permissions...)
		p.firms = []string{b.Code, tok.QuikFirm}
		p.info.BrokerID = b.ID
		p.info.BrokerCode = b.Code
		p.info.BrokerName = b.Name
		p.expires = time.Now().Add(24 * time.Hour)
		if tok.ExpiresAt != nil {
			p.expires = *tok.ExpiresAt
		}
		p.validate = func(check context.Context) error {
			current, newTok, err := s.Brokers.AuthenticateToken(check, raw, ip)
			if err != nil {
				return err
			}
			if current.ID != b.ID || current.Code != b.Code || newTok.ID != tok.ID || !slices.Equal(newTok.Permissions, tok.Permissions) || newTok.QuikFirm != tok.QuikFirm {
				return fmt.Errorf("token changed")
			}
			return nil
		}
		return p, nil
	}
	if s.Users == nil {
		return p, fmt.Errorf("admin realtime unavailable")
	}
	claims, err := s.Users.AuthenticateSession(ctx, s.JWTSecret, raw)
	if err != nil {
		return p, err
	}
	p.id = claims.UserID
	p.role = claims.RoleCode
	p.expires = claims.ExpiresAt.Time
	p.info.UserID = claims.UserID
	p.info.Email = claims.Email
	if len(md.Get("x-browser-bridge")) == 1 && strings.EqualFold(md.Get("x-browser-bridge")[0], "true") {
		p.info.Transport = "Browser → gRPC"
	}
	p.validate = func(check context.Context) error {
		return s.Users.ValidateSession(check, claims.UserID, claims.RoleCode, claims.UserVersion)
	}
	return p, nil
}
