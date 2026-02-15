// ABOUTME: Tests for the REST API CRUD endpoints.
// ABOUTME: Covers listing, creating, updating, deleting records, validation, and auth rejection.

package dynupdate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestAPIHandler(t *testing.T, opts ...StoreOption) (*APIServer, *Store) {
	t.Helper()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0, opts...)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	t.Cleanup(func() { s.Stop() })

	auth := &Auth{Token: "test-token"}
	api := NewAPIServer(s, auth, ":0", nil)
	return api, s
}

func TestAPI_ListRecords_Empty(t *testing.T) {
	t.Parallel()
	api, _ := newTestAPIHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp apiListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Records) != 0 {
		t.Errorf("got %d records, want 0", len(resp.Records))
	}
}

func TestAPI_CreateAndList(t *testing.T) {
	t.Parallel()
	api, _ := newTestAPIHandler(t)

	// Create
	body, _ := json.Marshal(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("create status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("list status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp apiListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Errorf("got %d records, want 1", len(resp.Records))
	}
}

func TestAPI_GetByName(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t)
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records/app.example.org.", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp apiListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Errorf("got %d records, want 1", len(resp.Records))
	}
}

func TestAPI_CreateValidationError(t *testing.T) {
	t.Parallel()
	api, _ := newTestAPIHandler(t)

	body, _ := json.Marshal(Record{Name: "bad", Type: "A", TTL: 300, Value: "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAPI_DeleteAll(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t)
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/records/app.example.org.", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	records := store.GetAll("app.example.org.")
	if len(records) != 0 {
		t.Errorf("got %d records after delete, want 0", len(records))
	}
}

func TestAPI_DeleteByNameAndType(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t)
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/records/app.example.org./A", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	// A should be gone; AAAA should remain
	aRecords := store.Get("app.example.org.", "A")
	if len(aRecords) != 0 {
		t.Errorf("A records = %d, want 0", len(aRecords))
	}
	aaaaRecords := store.Get("app.example.org.", "AAAA")
	if len(aaaaRecords) != 1 {
		t.Errorf("AAAA records = %d, want 1", len(aaaaRecords))
	}
}

func TestAPI_Upsert_PUT(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t)
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})

	body, _ := json.Marshal(Record{Name: "app.example.org.", Type: "A", TTL: 600, Value: "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	records := store.Get("app.example.org.", "A")
	if len(records) != 1 || records[0].TTL != 600 {
		t.Errorf("upsert failed: got %v", records)
	}
}

func TestAPI_Unauthorized(t *testing.T) {
	t.Parallel()
	api, _ := newTestAPIHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records", nil)
	// No auth header
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAPI_ListWithNameFilter(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t)
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = store.Upsert(Record{Name: "other.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/records?name=app.example.org.", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp apiListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Errorf("got %d records, want 1", len(resp.Records))
	}
}

func TestAPI_Create_PolicyUpdateOnly_Returns403(t *testing.T) {
	t.Parallel()
	api, _ := newTestAPIHandler(t, WithSyncPolicy(PolicyUpdateOnly))

	body, _ := json.Marshal(Record{Name: "a.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAPI_Update_PolicyCreateOnly_Returns403(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t, WithSyncPolicy(PolicyCreateOnly))
	// Seed a record directly to bypass policy
	store.mu.Lock()
	store.records["a.example.org."] = []Record{
		{Name: "a.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	}
	store.mu.Unlock()

	body, _ := json.Marshal(Record{Name: "a.example.org.", Type: "A", TTL: 600, Value: "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAPI_DeleteAll_PolicyUpsertOnly_Returns403(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t, WithSyncPolicy(PolicyUpsertOnly))
	store.mu.Lock()
	store.records["a.example.org."] = []Record{
		{Name: "a.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	}
	store.mu.Unlock()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/records/a.example.org.", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAPI_DeleteByType_PolicyUpsertOnly_Returns403(t *testing.T) {
	t.Parallel()
	api, store := newTestAPIHandler(t, WithSyncPolicy(PolicyUpsertOnly))
	store.mu.Lock()
	store.records["a.example.org."] = []Record{
		{Name: "a.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	}
	store.mu.Unlock()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/records/a.example.org./A", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAPI_Create_PolicySync_Returns201(t *testing.T) {
	t.Parallel()
	api, _ := newTestAPIHandler(t)

	body, _ := json.Marshal(Record{Name: "a.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/records", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}
