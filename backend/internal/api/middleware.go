package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/observability"

	"github.com/google/uuid"
)

func auditEntry(action, actorType, actorID string, brokerID *int64, entityID, ip, result, errMsg string) audit.Entry {
	return audit.Entry{
		ActorType:  actorType,
		ActorID:    actorID,
		BrokerID:   brokerID,
		Action:     action,
		EntityType: "api_token",
		EntityID:   entityID,
		IP:         ip,
		Result:     result,
		Error:      errMsg,
	}
}

type ctxKey string

const (
	ctxBroker      ctxKey = "broker"
	ctxAdmin       ctxKey = "admin"
	ctxTokenID     ctxKey = "token_id"
	ctxToken       ctxKey = "api_token"
	ctxPermissions ctxKey = "permissions"
)

type AdminPrincipal struct {
	UserID      string
	Email       string
	RoleCode    string
	UserVersion int64
	ExpiresAt   time.Time
}

func BrokerFromContext(ctx context.Context) (domain.Broker, bool) {
	b, ok := ctx.Value(ctxBroker).(domain.Broker)
	return b, ok
}

func BrokerIDFromContext(ctx context.Context) (int64, bool) {
	b, ok := BrokerFromContext(ctx)
	if !ok {
		return 0, false
	}
	return b.ID, true
}

func TokenIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ctxTokenID).(uuid.UUID)
	return id, ok
}

func TokenFromContext(ctx context.Context) (domain.APIToken, bool) {
	t, ok := ctx.Value(ctxToken).(domain.APIToken)
	return t, ok
}

func PermissionsFromContext(ctx context.Context) []string {
	p, _ := ctx.Value(ctxPermissions).([]string)
	return p
}

func AdminFromContext(ctx context.Context) (AdminPrincipal, bool) {
	a, ok := ctx.Value(ctxAdmin).(AdminPrincipal)
	return a, ok
}

func (s *Server) requireHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.RequireHTTPS {
			next.ServeHTTP(w, r)
			return
		}
		if observability.RequestProto(r, s.trusted) != "https" {
			writeErr(w, http.StatusForbidden, "HTTPS required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) brokerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, xff := s.clientIP(r)

		if s.authFailLimiter.Blocked(ip) {
			observability.AnnotateError(r, observability.AuthRateLimitExceeded, "too many failed authentication attempts", "auth")
			s.emitAuth(r, observability.AuthRateLimitExceeded, ip, nil, nil, "too many failed authentication attempts")
			writeErr(w, http.StatusTooManyRequests, "too many failed authentication attempts")
			return
		}

		raw := bearer(r)
		if raw == "" {
			s.authFailLimiter.Record(ip)
			s.recordAuthFail(r, ip, "missing_bearer", nil, nil)
			observability.AnnotateError(r, observability.AuthTokenNotFound, "missing bearer token", "auth")
			s.emitAuth(r, observability.AuthTokenNotFound, ip, nil, nil, "missing bearer token")
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		b, tok, err := s.brokers.AuthenticateToken(r.Context(), raw, ip)
		if err != nil {
			if errors.Is(err, brokers.ErrAuthenticationUnavailable) {
				writeErr(w, http.StatusServiceUnavailable, "authentication temporarily unavailable; request not accepted, retry later")
				return
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				s.writeInternal(w, err)
				return
			}
			var tid *uuid.UUID
			var bid *int64
			if tok.ID != uuid.Nil {
				tid = &tok.ID
				bid = &tok.BrokerID
			}
			reason := err.Error()
			if reason == "ip not allowed" {
				msg := observability.IPNotAllowedMessage(ip, xff)
				s.recordAuthFail(r, ip, msg, tid, bid)
				s.auditToken(r, "TOKEN_AUTH_FAILED", bid, tid, "denied", msg)
				if tok.ID != uuid.Nil {
					observability.AnnotateBroker(r, b.ID, b.Code, b.Name, tok.ID)
				}
				observability.AnnotateError(r, observability.AuthIPNotAllowed, msg, "auth")
				s.emitAuth(r, observability.AuthIPNotAllowed, ip, bid, tid, msg)
				writeErr(w, http.StatusForbidden, msg)
				return
			}
			s.authFailLimiter.Record(ip)
			s.recordAuthFail(r, ip, reason, tid, bid)
			action := "TOKEN_AUTH_FAILED"
			sub := observability.AuthSubtypeFromReason(reason)
			if reason == "token expired" {
				action = "TOKEN_EXPIRED"
			}
			s.auditToken(r, action, bid, tid, "denied", reason)
			observability.AnnotateError(r, sub, reason, "auth")
			s.emitAuth(r, sub, ip, bid, tid, reason)
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}

		if s.cfg.HMACRequired || r.Header.Get("X-Signature") != "" || r.Header.Get("X-Timestamp") != "" || r.Header.Get("X-Nonce") != "" {
			if err := s.verifyOptionalHMAC(r, raw); err != nil {
				bid := b.ID
				tid := tok.ID
				s.authFailLimiter.Record(ip)
				s.recordAuthFail(r, ip, "hmac_failed", &tid, &bid)
				s.auditToken(r, "TOKEN_AUTH_FAILED", &bid, &tid, "denied", "hmac_failed")
				sub := observability.AuthHMACFailed
				if errors.Is(err, auth.ErrNonceReplay) {
					sub = observability.AuthNonceReplay
				}
				observability.AnnotateError(r, sub, "invalid signature", "auth")
				s.emitAuth(r, sub, ip, &bid, &tid, "hmac_failed")
				writeErr(w, http.StatusUnauthorized, "invalid signature")
				return
			}
		}

		bid := b.ID
		tid := tok.ID
		s.auditAuthSuccessThrottled(r, &bid, &tid)
		observability.AnnotateBroker(r, b.ID, b.Code, b.Name, tok.ID)

		ctx := context.WithValue(r.Context(), ctxBroker, b)
		ctx = context.WithValue(ctx, ctxTokenID, tok.ID)
		ctx = context.WithValue(ctx, ctxToken, tok)
		ctx = context.WithValue(ctx, ctxPermissions, tok.Permissions)
		s.admitBroker(next).ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			perms := PermissionsFromContext(r.Context())
			if !auth.HasPermission(perms, perm) {
				b, _ := BrokerFromContext(r.Context())
				tid, _ := TokenIDFromContext(r.Context())
				bid := b.ID
				s.auditToken(r, "TOKEN_PERMISSION_DENIED", &bid, &tid, "denied", perm)
				observability.AnnotateError(r, observability.AuthPermissionDenied, perm, "auth")
				ip, _ := s.clientIP(r)
				s.emitAuth(r, observability.AuthPermissionDenied, ip, &bid, &tid, perm)
				writeErr(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) verifyOptionalHMAC(r *http.Request, secret string) error {
	sig := r.Header.Get("X-Signature")
	ts := r.Header.Get("X-Timestamp")
	nonce := r.Header.Get("X-Nonce")
	if sig == "" || ts == "" || nonce == "" {
		if s.cfg.HMACRequired || sig != "" || ts != "" || nonce != "" {
			return errHMAC("missing hmac headers")
		}
		return nil
	}
	if err := auth.ValidateTimestamp(ts, 5*time.Minute); err != nil {
		return err
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return errHMAC("body too large")
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	payload := auth.BuildHMACPayload(ts, nonce, r.Method, r.URL.RequestURI(), string(body))
	if !auth.VerifyHMACSHA256(secret, payload, sig) {
		return errHMAC("bad signature")
	}
	if s.hmacNonces != nil {
		if err := s.hmacNonces.UseScoped(auth.HashToken(secret, "hmac-nonce"), nonce); err != nil {
			return err
		}
	}
	return nil
}

type hmacError string

func (e hmacError) Error() string { return string(e) }
func errHMAC(s string) error      { return hmacError(s) }

func (s *Server) recordAuthFail(r *http.Request, ip, reason string, tokenID *uuid.UUID, brokerID *int64) {
	s.brokers.RecordAuthFailure(r.Context(), ip, r.URL.Path, reason, tokenID, brokerID)
}

func (s *Server) auditToken(r *http.Request, action string, brokerID *int64, tokenID *uuid.UUID, result, errMsg string) {
	entityID := ""
	if tokenID != nil {
		entityID = tokenID.String()
	}
	actorID := ""
	if admin, ok := AdminFromContext(r.Context()); ok {
		actorID = admin.UserID
	} else if tokenID != nil {
		actorID = tokenID.String()
	}
	actorType := "system"
	if admin, ok := AdminFromContext(r.Context()); ok {
		actorType = "admin"
		_ = admin
	} else if tokenID != nil {
		actorType = "broker"
	}
	ip, _ := s.clientIP(r)
	_ = s.audit.Write(r.Context(), auditEntry(action, actorType, actorID, brokerID, entityID, ip, result, errMsg))
}

func (s *Server) auditAuthSuccessThrottled(r *http.Request, brokerID *int64, tokenID *uuid.UUID) {
	if tokenID == nil {
		return
	}
	key := tokenID.String()
	if v, ok := s.authSuccessThrottle.Load(key); ok {
		if t, ok := v.(time.Time); ok && time.Since(t) < time.Minute {
			return
		}
	}
	s.authSuccessThrottle.Store(key, time.Now())
	s.auditToken(r, "TOKEN_AUTH_SUCCESS", brokerID, tokenID, "ok", "")
	ip, _ := s.clientIP(r)
	s.emitAuth(r, observability.AuthSuccess, ip, brokerID, tokenID, "")
}

func (s *Server) emitAuth(r *http.Request, subtype, ip string, brokerID *int64, tokenID *uuid.UUID, reason string) {
	_, xff := s.clientIP(r)
	e := observability.Event{
		Type: observability.TypeAuth, Subtype: subtype, ClientIP: ip, ForwardedFor: xff,
		UserAgent: r.UserAgent(), Endpoint: r.URL.Path,
		HTTPMethod: r.Method, RequestID: observability.RequestIDFrom(r.Context()),
		TraceID: observability.TraceIDFrom(r.Context()), BrokerID: brokerID, TokenID: tokenID,
		Message: reason, ErrorCode: subtype, ErrorMessage: reason, FailureStage: "auth",
	}
	if subtype == observability.AuthSuccess {
		e.Result = observability.ResultSuccess
		e.ErrorCode = ""
		e.ErrorMessage = ""
		e.FailureStage = ""
	}
	s.emitObs(e)
}

func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearer(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := s.users.AuthenticateSession(r.Context(), s.cfg.JWTSecret, raw)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				s.writeInternal(w, err)
				return
			}
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxAdmin, AdminPrincipal{
			UserID: claims.UserID, Email: claims.Email, RoleCode: claims.RoleCode, UserVersion: claims.UserVersion, ExpiresAt: claims.ExpiresAt.Time,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdminRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admin, ok := AdminFromContext(r.Context())
			if !ok {
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if !auth.HasAnyRole(admin.RoleCode, roles...) {
				writeErr(w, http.StatusForbidden, "insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) adminRO(h http.HandlerFunc) http.Handler {
	return s.adminAuth(s.requireAdminRole(auth.RolesAll...)(h))
}

func (s *Server) adminWrite(h http.HandlerFunc) http.Handler {
	return s.adminAuth(s.requireAdminRole(auth.RolesWrite...)(h))
}

func (s *Server) adminOps(h http.HandlerFunc) http.Handler {
	return s.adminAuth(s.requireAdminRole(auth.RolesOps...)(h))
}

func (s *Server) adminAudit(h http.HandlerFunc) http.Handler {
	return s.adminAuth(s.requireAdminRole(auth.RolesAudit...)(h))
}

func (s *Server) adminUsers(h http.HandlerFunc) http.Handler {
	return s.adminAuth(s.requireAdminRole(auth.RolesUsers...)(h))
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range s.cfg.CORSOrigins {
		allowed[strings.TrimSpace(o)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Timestamp, X-Nonce, X-Signature, X-Request-ID, X-Trace-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-Trace-ID, Retry-After")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if observability.RequestProto(r, s.trusted) == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
