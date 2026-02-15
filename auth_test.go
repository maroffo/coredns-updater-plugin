// ABOUTME: Tests for dual-mode authentication: Bearer token + mTLS CN validation.
// ABOUTME: Covers HTTP middleware, gRPC interceptor, constant-time comparison, and edge cases.

package dynupdate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestAuth_HTTPMiddleware_ValidToken(t *testing.T) {
	t.Parallel()
	auth := &Auth{Token: "secret-token"}

	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuth_HTTPMiddleware_InvalidToken(t *testing.T) {
	t.Parallel()
	auth := &Auth{Token: "secret-token"}

	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuth_HTTPMiddleware_MissingToken(t *testing.T) {
	t.Parallel()
	auth := &Auth{Token: "secret-token"}

	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuth_HTTPMiddleware_mTLS_ValidCN(t *testing.T) {
	t.Parallel()
	auth := &Auth{AllowedCN: []string{"client.example.org"}}

	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{Subject: pkix.Name{CommonName: "client.example.org"}},
		},
	}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuth_HTTPMiddleware_mTLS_InvalidCN(t *testing.T) {
	t.Parallel()
	auth := &Auth{AllowedCN: []string{"client.example.org"}}

	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{Subject: pkix.Name{CommonName: "rogue.example.org"}},
		},
	}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuth_HTTPMiddleware_NoAuthConfigured_FailsClosed(t *testing.T) {
	t.Parallel()
	// No token, no CN, no NoAuth flag: must reject (fail-closed)
	auth := &Auth{}

	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (fail-closed)", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuth_HTTPMiddleware_NoAuth_ExplicitOptOut(t *testing.T) {
	t.Parallel()
	// NoAuth explicitly set: allow all
	auth := &Auth{NoAuth: true}

	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuth_GRPCInterceptor_NoAuth_FailsClosed(t *testing.T) {
	t.Parallel()
	// No token, no CN, no NoAuth: must reject
	auth := &Auth{}

	ctx := context.Background()
	_, err := auth.UnaryInterceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error (fail-closed)")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", err)
	}
}

func TestAuth_GRPCInterceptor_NoAuth_ExplicitOptOut(t *testing.T) {
	t.Parallel()
	auth := &Auth{NoAuth: true}

	ctx := context.Background()
	called := false
	_, err := auth.UnaryInterceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("UnaryInterceptor() error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestAuth_GRPCInterceptor_ValidToken(t *testing.T) {
	t.Parallel()
	auth := &Auth{Token: "grpc-secret"}

	md := metadata.Pairs("authorization", "Bearer grpc-secret")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}

	resp, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
	if err != nil {
		t.Fatalf("UnaryInterceptor() error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
	if resp != "ok" {
		t.Errorf("response = %v, want ok", resp)
	}
}

func TestAuth_GRPCInterceptor_InvalidToken(t *testing.T) {
	t.Parallel()
	auth := &Auth{Token: "grpc-secret"}

	md := metadata.Pairs("authorization", "Bearer wrong-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := auth.UnaryInterceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", err)
	}
}

func TestAuth_GRPCInterceptor_mTLS_ValidCN(t *testing.T) {
	t.Parallel()
	auth := &Auth{AllowedCN: []string{"grpc-client.example.org"}}

	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{
				{Subject: pkix.Name{CommonName: "grpc-client.example.org"}},
			},
		},
	}
	p := &peer.Peer{AuthInfo: tlsInfo}
	ctx := peer.NewContext(context.Background(), p)

	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}

	_, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
	if err != nil {
		t.Fatalf("UnaryInterceptor() error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestAuth_GRPCInterceptor_NoAuth(t *testing.T) {
	t.Parallel()
	auth := &Auth{Token: "required"}

	ctx := context.Background()
	_, err := auth.UnaryInterceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", err)
	}
}
