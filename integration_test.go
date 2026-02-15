// ABOUTME: Integration tests: end-to-end API → Store → DNS flows.
// ABOUTME: Tests concurrent access, auth rejection, file reload, and full CRUD lifecycle.

package dynupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
)

// TestIntegration_API_To_DNS creates records via the REST API and queries them via DNS.
func TestIntegration_API_To_DNS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	store, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer store.Stop()

	auth := &Auth{Token: "integration-token"}
	api := NewAPIServer(store, auth, ":0", nil)

	handler := &DynUpdate{
		Zones: []string{"example.org."},
		Store: store,
	}

	// Create A record via API
	body, _ := json.Marshal(Record{Name: "web.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer integration-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("API create: status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	// Query via DNS
	dnsReq := new(dns.Msg)
	dnsReq.SetQuestion("web.example.org.", dns.TypeA)
	dnsRec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := handler.ServeDNS(context.Background(), dnsRec, dnsReq)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("DNS rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	if len(dnsRec.Msg.Answer) != 1 {
		t.Fatalf("DNS answers = %d, want 1", len(dnsRec.Msg.Answer))
	}
	a, ok := dnsRec.Msg.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer type = %T, want *dns.A", dnsRec.Msg.Answer[0])
	}
	if a.A.String() != "10.0.0.1" {
		t.Errorf("A = %s, want 10.0.0.1", a.A)
	}
}

// TestIntegration_CNAME_Chain tests API creation of a CNAME chain and DNS resolution.
func TestIntegration_CNAME_Chain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	store, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer store.Stop()

	auth := &Auth{Token: "token"}
	api := NewAPIServer(store, auth, ":0", nil)

	handler := &DynUpdate{
		Zones: []string{"example.org."},
		Store: store,
	}

	// Create CNAME and target A record
	for _, rec := range []Record{
		{Name: "cdn.example.org.", Type: "CNAME", TTL: 300, Value: "origin.example.org."},
		{Name: "origin.example.org.", Type: "A", TTL: 300, Value: "10.0.0.50"},
	} {
		body, _ := json.Marshal(rec)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.handler().ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", rec.Name, w.Code)
		}
	}

	// Query cdn.example.org. A
	dnsReq := new(dns.Msg)
	dnsReq.SetQuestion("cdn.example.org.", dns.TypeA)
	dnsRec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := handler.ServeDNS(context.Background(), dnsRec, dnsReq)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	if len(dnsRec.Msg.Answer) != 2 {
		t.Fatalf("answers = %d, want 2 (CNAME + A)", len(dnsRec.Msg.Answer))
	}
}

// TestIntegration_ConcurrentAPIAndDNS exercises concurrent API writes and DNS reads.
func TestIntegration_ConcurrentAPIAndDNS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	store, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer store.Stop()

	auth := &Auth{Token: "token"}
	api := NewAPIServer(store, auth, ":0", nil)

	handler := &DynUpdate{
		Zones: []string{"example.org."},
		Store: store,
	}

	// Seed an initial record
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})

	var wg sync.WaitGroup

	// Concurrent DNS readers
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dnsReq := new(dns.Msg)
			dnsReq.SetQuestion("app.example.org.", dns.TypeA)
			dnsRec := dnstest.NewRecorder(&test.ResponseWriter{})
			_, _ = handler.ServeDNS(context.Background(), dnsRec, dnsReq)
		}()
	}

	// Concurrent API writers
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := Record{
				Name:  "app.example.org.",
				Type:  "TXT",
				TTL:   300,
				Value: "concurrent-" + string(rune('a'+i%26)),
			}
			body, _ := json.Marshal(rec)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			api.handler().ServeHTTP(w, req)
		}(i)
	}

	wg.Wait()
	// No panics or races = success
}

// TestIntegration_AuthRejection verifies unauthenticated API requests are rejected.
func TestIntegration_AuthRejection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	store, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer store.Stop()

	auth := &Auth{Token: "secret"}
	api := NewAPIServer(store, auth, ":0", nil)

	tests := []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{"no token GET", http.MethodGet, "/api/v1/records", "", http.StatusUnauthorized},
		{"wrong token GET", http.MethodGet, "/api/v1/records", "wrong", http.StatusUnauthorized},
		{"valid token GET", http.MethodGet, "/api/v1/records", "secret", http.StatusOK},
		{"no token POST", http.MethodPost, "/api/v1/records", "", http.StatusUnauthorized},
		{"no token DELETE", http.MethodDelete, "/api/v1/records/test.example.org.", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			api.handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// TestIntegration_DeleteAndVerifyDNS creates, deletes, and verifies NXDOMAIN.
func TestIntegration_DeleteAndVerifyDNS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	store, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer store.Stop()

	auth := &Auth{Token: "token"}
	api := NewAPIServer(store, auth, ":0", nil)

	handler := &DynUpdate{
		Zones: []string{"example.org."},
		Store: store,
	}

	// Create
	body, _ := json.Marshal(Record{Name: "ephemeral.example.org.", Type: "A", TTL: 300, Value: "10.0.0.99"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status = %d", w.Code)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/records/ephemeral.example.org.", nil)
	req.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	api.handler().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", w.Code)
	}

	// DNS should return NXDOMAIN
	dnsReq := new(dns.Msg)
	dnsReq.SetQuestion("ephemeral.example.org.", dns.TypeA)
	dnsRec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := handler.ServeDNS(context.Background(), dnsRec, dnsReq)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeNameError {
		t.Errorf("rcode = %d, want %d (NXDOMAIN)", code, dns.RcodeNameError)
	}
}
