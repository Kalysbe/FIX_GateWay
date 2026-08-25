package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/ratelimit"
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
	UserID   string
	Email    string
	RoleCode string
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
		proto := r.Header.Get("X-Forwarded-Proto")
		if proto == "" {
			if r.TLS != nil {
				proto = "https"
			} else {
				proto = "http"
			}
		}
		if !strings.EqualFold(proto, "https") {
			writeErr(w, http.StatusForbidden, "HTTPS required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) brokerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

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
			var tid *uuid.UUID
			var bid *int64
			if tok.ID != uuid.Nil {
				tid = &tok.ID
				bid = &tok.BrokerID
			}
			reason := err.Error()
			if reason == "ip not allowed" {
				s.recordAuthFail(r, ip, reason, tid, bid)
				s.auditToken(r, "TOKEN_AUTH_FAILED", bid, tid, "denied", reason)
				observability.AnnotateError(r, observability.AuthIPNotAllowed, reason, "auth")
				s.emitAuth(r, observability.AuthIPNotAllowed, ip, bid, tid, reason)
				writeErr(w, http.StatusForbidden, "ip not allowed")
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

		if s.cfg.HMACRequired || r.Header.Get("X-Signature") != "" {
			if err := s.verifyOptionalHMAC(r, raw); err != nil {
				bid := b.ID
				tid := tok.ID
				s.authFailLimiter.Record(ip)
				s.recordAuthFail(r, ip, "hmac_failed", &tid, &bid)
				s.auditToken(r, "TOKEN_AUTH_FAILED", &bid, &tid, "denied", "hmac_failed")
				observability.AnnotateError(r, observability.AuthHMACFailed, "invalid signature", "auth")
				s.emitAuth(r, observability.AuthHMACFailed, ip, &bid, &tid, "hmac_failed")
				writeErr(w, http.StatusUnauthorized, "invalid signature")
				return
			}
		}

		rps := b.RateLimitRPS
		if rps <= 0 {
			rps = s.cfg.DefaultRateLimitRPS
		}
		reqKey := "req:" + tok.ID.String()
		if lim, ok := s.brokerReqLimiters.Load(reqKey); ok {
			if !lim.(*ratelimit.Limiter).Allow(reqKey) {
				observability.AnnotateError(r, observability.AuthRateLimitExceeded, "rate limit exceeded", "auth")
				s.emitAuth(r, observability.AuthRateLimitExceeded, ip, &b.ID, &tok.ID, "rate limit exceeded")
				writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		} else {
			lim := ratelimit.New(rps, rps)
			s.brokerReqLimiters.Store(reqKey, lim)
			if !lim.Allow(reqKey) {
				observability.AnnotateBroker(r, b.ID, b.Code, b.Name, tok.ID)
				observability.AnnotateError(r, observability.AuthRateLimitExceeded, "rate limit exceeded", "auth")
				s.emitAuth(r, observability.AuthRateLimitExceeded, ip, &b.ID, &tok.ID, "rate limit exceeded")
				writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
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
		next.ServeHTTP(w, r.WithContext(ctx))
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
				s.emitAuth(r, observability.AuthPermissionDenied, clientIP(r), &bid, &tid, perm)
				writeErr(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) orderRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := BrokerFromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		tid, _ := TokenIDFromContext(r.Context())
		ops := b.RateLimitOrdersPerSec
		if ops <= 0 {
			ops = s.cfg.DefaultOrderRateLimit
		}
		key := "ord:" + tid.String()
		var lim *ratelimit.Limiter
		if v, ok := s.brokerOrderLimiters.Load(key); ok {
			lim = v.(*ratelimit.Limiter)
		} else {
			lim = ratelimit.New(ops, ops)
			s.brokerOrderLimiters.Store(key, lim)
		}
		if !lim.Allow(key) {
			observability.AnnotateError(r, observability.AuthRateLimitExceeded, "order rate limit exceeded", "auth")
			s.emitAuth(r, observability.AuthRateLimitExceeded, clientIP(r), &b.ID, &tid, "order rate limit exceeded")
			writeErr(w, http.StatusTooManyRequests, "order rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) verifyOptionalHMAC(r *http.Request, secret string) error {
	sig := r.Header.Get("X-Signature")
	ts := r.Header.Get("X-Timestamp")
	nonce := r.Header.Get("X-Nonce")
	if sig == "" || ts == "" || nonce == "" {
		if s.cfg.HMACRequired {
			return errHMAC("missing hmac headers")
		}
		return nil
	}
	if err := auth.ValidateTimestamp(ts, 5*time.Minute); err != nil {
		return err
	}
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	payload := auth.BuildHMACPayload(ts, nonce, r.Method, r.URL.Path, string(body))
	if !auth.VerifyHMACSHA256(secret, payload, sig) {
		return errHMAC("bad signature")
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
	_ = s.audit.Write(r.Context(), auditEntry(action, actorType, actorID, brokerID, entityID, clientIP(r), result, errMsg))
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
	s.emitAuth(r, observability.AuthSuccess, clientIP(r), brokerID, tokenID, "")
}

func (s *Server) emitAuth(r *http.Request, subtype, ip string, brokerID *int64, tokenID *uuid.UUID, reason string) {
	e := observability.Event{
		Type: observability.TypeAuth, Subtype: subtype, ClientIP: ip, Endpoint: r.URL.Path,
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
		claims, err := auth.ParseAdminJWT(s.cfg.JWTSecret, raw)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxAdmin, AdminPrincipal{
			UserID: claims.UserID, Email: claims.Email, RoleCode: claims.RoleCode,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		// handle host:port and [ipv6]:port
		if strings.Count(host, ":") == 1 || strings.HasPrefix(host, "[") {
			return strings.Trim(host[:i], "[]")
		}
	}
	return host
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
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-Trace-ID")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
