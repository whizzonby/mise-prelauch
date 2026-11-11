package httpx

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	clientIPKey
	traceIDKey
	clientUAKey
)

type Middleware func(http.Handler) http.Handler

// Chain applies middleware so the first listed runs outermost.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	return ip
}

// ClientUserAgent is the end user's browser, even when the request was made on
// their behalf by one of Mise's Next.js servers.
func ClientUserAgent(ctx context.Context) string {
	ua, _ := ctx.Value(clientUAKey).(string)
	return ua
}

// Headers a Mise backend-for-frontend (the admin and customer Next.js
// servers) uses to pass on the address and browser of the person it is acting
// for. They are believed only alongside the shared BFF key; otherwise every
// sign-in would appear to come from the Next.js server, and per-address rate
// limits would become one global limit.
const (
	HeaderBFFKey      = "X-Mise-Bff-Key"
	HeaderBFFClientIP = "X-Mise-Client-Ip"
	HeaderBFFClientUA = "X-Mise-Client-Ua"
)

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// W3C Trace Context: version-traceid-parentid-flags. Only version 00 is read.
var validTraceparent = regexp.MustCompile(`^00-([0-9a-f]{32})-[0-9a-f]{16}-[0-9a-f]{2}$`)

// TraceID returns the W3C trace id the caller sent, if any. Logs carry it so
// that, once an OpenTelemetry collector exists, log lines join traces without
// changing any handler (ADR-005).
func TraceID(ctx context.Context) string {
	id, _ := ctx.Value(traceIDKey).(string)
	return id
}

// WithRequestID accepts a well-formed inbound X-Request-Id (so a trace can span
// the Next.js apps and the API) or generates one.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID.MatchString(id) {
			buf := make([]byte, 12)
			_, _ = rand.Read(buf)
			id = hex.EncodeToString(buf)
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		if m := validTraceparent.FindStringSubmatch(r.Header.Get("Traceparent")); m != nil && m[1] != strings.Repeat("0", 32) {
			ctx = context.WithValue(ctx, traceIDKey, m[1])
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// WithClientIP resolves the caller's address. Behind N trusted proxies the
// client is the Nth entry from the right of X-Forwarded-For; anything further
// left is caller-controlled and ignored. A request carrying the BFF key may
// name the end user's address and browser instead (see HeaderBFFKey).
func WithClientIP(trustedHops int, bffKey string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if trustedHops > 0 {
				parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
				if idx := len(parts) - trustedHops; idx >= 0 {
					if candidate := strings.TrimSpace(parts[idx]); net.ParseIP(candidate) != nil {
						ip = candidate
					}
				}
			}
			ua := r.UserAgent()
			if bffKey != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get(HeaderBFFKey)), []byte(bffKey)) == 1 {
				if candidate := strings.TrimSpace(r.Header.Get(HeaderBFFClientIP)); net.ParseIP(candidate) != nil {
					ip = candidate
				}
				ua = r.Header.Get(HeaderBFFClientUA)
			}
			ctx := context.WithValue(r.Context(), clientIPKey, ip)
			ctx = context.WithValue(ctx, clientUAKey, ua)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// WithAccessLog logs one line per request. It records the path but never the
// query string, headers or body, which can carry tokens and personal data.
func WithAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		}
		slog.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"route", r.Pattern,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// WithRecover turns a panic into a 500 without leaking the stack to the client.
func WithRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.ErrorContext(r.Context(), "panic recovered", "panic", rec, "stack", string(debug.Stack()))
				Fail(w, r, NewError(http.StatusInternalServerError, "internal_error", "Something went wrong on our side."))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// WithSecurityHeaders sets headers appropriate for a JSON API.
func WithSecurityHeaders(production bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cross-Origin-Resource-Policy", "cross-origin")
			if production {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithCORS allows only the configured origins. Credentials are never allowed:
// browsers authenticate with bearer tokens, not cookies, so CSRF does not apply.
func WithCORS(allowedOrigins []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			w.Header().Add("Vary", "Origin")
			if origin != "" && slices.Contains(allowedOrigins, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-Id")
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, Traceparent")
					w.Header().Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
