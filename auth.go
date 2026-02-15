// ABOUTME: Dual-mode authentication: Bearer token + mTLS CN validation.
// ABOUTME: Provides HTTP middleware and gRPC unary interceptor for access control.

package dynupdate

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Auth holds authentication configuration for the management APIs.
type Auth struct {
	Token     string
	AllowedCN []string
}

// authRequired returns true when at least one auth mechanism is configured.
func (a *Auth) authRequired() bool {
	return a.Token != "" || len(a.AllowedCN) > 0
}

// HTTPMiddleware returns an http.Handler that validates Bearer token or mTLS CN
// before calling next.
func (a *Auth) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authRequired() {
			next.ServeHTTP(w, r)
			return
		}

		// Try Bearer token
		if a.Token != "" {
			if token := extractBearerHTTP(r); token != "" {
				if constantTimeEqual(token, a.Token) {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Try mTLS CN
		if len(a.AllowedCN) > 0 {
			if cn := extractCNFromTLS(r.TLS); cn != "" {
				if a.cnAllowed(cn) {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// UnaryInterceptor is a gRPC interceptor that validates Bearer token or mTLS CN.
func (a *Auth) UnaryInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if !a.authRequired() {
		return handler(ctx, req)
	}

	// Try Bearer token from metadata
	if a.Token != "" {
		if token := extractBearerGRPC(ctx); token != "" {
			if constantTimeEqual(token, a.Token) {
				return handler(ctx, req)
			}
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
	}

	// Try mTLS CN from peer
	if len(a.AllowedCN) > 0 {
		if cn := extractCNFromPeer(ctx); cn != "" {
			if a.cnAllowed(cn) {
				return handler(ctx, req)
			}
		}
	}

	return nil, status.Error(codes.Unauthenticated, "authentication required")
}

func (a *Auth) cnAllowed(cn string) bool {
	for _, allowed := range a.AllowedCN {
		if allowed == cn {
			return true
		}
	}
	return false
}

func extractBearerHTTP(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func extractBearerGRPC(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return ""
	}
	h := vals[0]
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func extractCNFromTLS(state *tls.ConnectionState) string {
	if state == nil || len(state.PeerCertificates) == 0 {
		return ""
	}
	return state.PeerCertificates[0].Subject.CommonName
}

func extractCNFromPeer(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return ""
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return ""
	}
	return extractCNFromTLS(&tlsInfo.State)
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
