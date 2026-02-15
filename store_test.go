// ABOUTME: Tests for the thread-safe in-memory Store with JSON persistence.
// ABOUTME: Covers CRUD, concurrency, atomic persistence, auto-reload, and readiness.

package dynupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStore_NewAndReady(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	if !s.Ready() {
		t.Error("Ready() = false after NewStore, want true")
	}
}

func TestStore_NewWithExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	data := storeFile{Records: []Record{
		{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	}}
	raw, _ := json.MarshalIndent(data, "", "  ")
	if err := os.WriteFile(fp, raw, 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	records := s.Get("app.example.org.", "A")
	if len(records) != 1 {
		t.Fatalf("Get() returned %d records, want 1", len(records))
	}
	if records[0].Value != "10.0.0.1" {
		t.Errorf("Value = %q, want %q", records[0].Value, "10.0.0.1")
	}
}

func TestStore_Upsert_Insert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	r := Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"}
	if err := s.Upsert(r); err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}

	records := s.Get("app.example.org.", "A")
	if len(records) != 1 {
		t.Fatalf("Get() returned %d records, want 1", len(records))
	}
	if records[0].Value != "10.0.0.1" {
		t.Errorf("Value = %q, want %q", records[0].Value, "10.0.0.1")
	}
}

func TestStore_Upsert_Update(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	r := Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"}
	if err := s.Upsert(r); err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}

	// Upsert same name+type+value with different TTL => update
	r2 := Record{Name: "app.example.org.", Type: "A", TTL: 600, Value: "10.0.0.1"}
	if err := s.Upsert(r2); err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}

	records := s.Get("app.example.org.", "A")
	if len(records) != 1 {
		t.Fatalf("Get() returned %d records, want 1 (upsert should update, not duplicate)", len(records))
	}
	if records[0].TTL != 600 {
		t.Errorf("TTL = %d, want 600", records[0].TTL)
	}
}

func TestStore_Upsert_MultipleValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	r1 := Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"}
	r2 := Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"}
	if err := s.Upsert(r1); err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}
	if err := s.Upsert(r2); err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}

	records := s.Get("app.example.org.", "A")
	if len(records) != 2 {
		t.Fatalf("Get() returned %d records, want 2", len(records))
	}
}

func TestStore_GetAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	_ = s.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s.Upsert(Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"})
	_ = s.Upsert(Record{Name: "other.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})

	all := s.GetAll("app.example.org.")
	if len(all) != 2 {
		t.Errorf("GetAll() returned %d records, want 2", len(all))
	}
}

func TestStore_List(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	_ = s.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s.Upsert(Record{Name: "other.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})

	all := s.List()
	if len(all) != 2 {
		t.Errorf("List() returned %d records, want 2", len(all))
	}
}

func TestStore_Delete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	_ = s.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})

	if err := s.Delete("app.example.org.", "A", "10.0.0.1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	records := s.Get("app.example.org.", "A")
	if len(records) != 1 {
		t.Fatalf("Get() returned %d records after delete, want 1", len(records))
	}
	if records[0].Value != "10.0.0.2" {
		t.Errorf("remaining Value = %q, want %q", records[0].Value, "10.0.0.2")
	}
}

func TestStore_DeleteAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	_ = s.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s.Upsert(Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"})

	if err := s.DeleteAll("app.example.org."); err != nil {
		t.Fatalf("DeleteAll() error: %v", err)
	}

	all := s.GetAll("app.example.org.")
	if len(all) != 0 {
		t.Errorf("GetAll() returned %d records after DeleteAll, want 0", len(all))
	}
}

func TestStore_JSONPersistence_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s1, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	_ = s1.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s1.Upsert(Record{Name: "mail.example.org.", Type: "MX", TTL: 3600, Value: "mx1.example.org.", Priority: 10})
	s1.Stop()

	// Open a new store from the same file
	s2, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() #2 error: %v", err)
	}
	defer s2.Stop()

	records := s2.Get("app.example.org.", "A")
	if len(records) != 1 || records[0].Value != "10.0.0.1" {
		t.Errorf("persistence round-trip failed for A record: got %v", records)
	}
	mxRecords := s2.Get("mail.example.org.", "MX")
	if len(mxRecords) != 1 || mxRecords[0].Priority != 10 {
		t.Errorf("persistence round-trip failed for MX record: got %v", mxRecords)
	}
}

func TestStore_AutoReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	// Externally write a record
	data := storeFile{Records: []Record{
		{Name: "external.example.org.", Type: "A", TTL: 300, Value: "10.0.0.99"},
	}}
	raw, _ := json.MarshalIndent(data, "", "  ")

	// Wait a moment so the mtime changes
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(fp, raw, 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Wait for reload cycle
	time.Sleep(300 * time.Millisecond)

	records := s.Get("external.example.org.", "A")
	if len(records) != 1 {
		t.Fatalf("auto-reload: Get() returned %d records, want 1", len(records))
	}
	if records[0].Value != "10.0.0.99" {
		t.Errorf("auto-reload: Value = %q, want %q", records[0].Value, "10.0.0.99")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	var wg sync.WaitGroup
	// Writers
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := Record{
				Name:  "app.example.org.",
				Type:  "A",
				TTL:   300,
				Value: "10.0.0." + string(rune('0'+i%10)),
			}
			_ = s.Upsert(r)
		}(i)
	}
	// Readers
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Get("app.example.org.", "A")
			_ = s.GetAll("app.example.org.")
			_ = s.List()
		}()
	}
	wg.Wait()
}

func TestStore_Get_EmptyStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	records := s.Get("nonexistent.example.org.", "A")
	if len(records) != 0 {
		t.Errorf("Get() on empty store returned %d records, want 0", len(records))
	}
}

func TestStore_CheckReload_NoDataLoss(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	// Upsert 100 records concurrently while reload is running
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := Record{
				Name:  fmt.Sprintf("host-%d.example.org.", i),
				Type:  "A",
				TTL:   300,
				Value: fmt.Sprintf("10.0.%d.%d", i/256, i%256),
			}
			if uErr := s.Upsert(r); uErr != nil {
				t.Errorf("Upsert(%d) error: %v", i, uErr)
			}
		}(i)
	}
	wg.Wait()

	// Allow a few reload cycles to run
	time.Sleep(200 * time.Millisecond)

	// All 100 records must still be present
	all := s.List()
	if len(all) != 100 {
		t.Errorf("List() returned %d records after concurrent Upsert+reload, want 100", len(all))
	}
}

func TestStore_DeleteByType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	_ = s.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})
	_ = s.Upsert(Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"})

	if err := s.DeleteByType("app.example.org.", "A"); err != nil {
		t.Fatalf("DeleteByType() error: %v", err)
	}

	aRecords := s.Get("app.example.org.", "A")
	if len(aRecords) != 0 {
		t.Errorf("A records = %d, want 0", len(aRecords))
	}

	aaaaRecords := s.Get("app.example.org.", "AAAA")
	if len(aaaaRecords) != 1 {
		t.Errorf("AAAA records = %d, want 1", len(aaaaRecords))
	}
}

func TestStore_Delete_NonExistent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	// Should not error when deleting non-existent records
	if err := s.Delete("nonexistent.example.org.", "A", "10.0.0.1"); err != nil {
		t.Errorf("Delete() non-existent record returned error: %v", err)
	}
}

func TestStore_MaxRecords_RejectsNew(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0, WithMaxRecords(2))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	_ = s.Upsert(Record{Name: "a.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s.Upsert(Record{Name: "b.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})

	err = s.Upsert(Record{Name: "c.example.org.", Type: "A", TTL: 300, Value: "10.0.0.3"})
	if err == nil {
		t.Fatal("Upsert() expected error when limit reached")
	}
}

func TestStore_MaxRecords_UpdatesAllowed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0, WithMaxRecords(2))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	_ = s.Upsert(Record{Name: "a.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = s.Upsert(Record{Name: "b.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})

	// Update existing record (same name+type+value, different TTL) — should succeed
	err = s.Upsert(Record{Name: "a.example.org.", Type: "A", TTL: 600, Value: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Upsert(update) error: %v", err)
	}

	records := s.Get("a.example.org.", "A")
	if len(records) != 1 || records[0].TTL != 600 {
		t.Errorf("update failed: got %v", records)
	}
}

func TestStore_MaxRecords_ZeroUnlimited(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0, WithMaxRecords(0))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	for i := range 100 {
		err := s.Upsert(Record{Name: fmt.Sprintf("host-%d.example.org.", i), Type: "A", TTL: 300, Value: fmt.Sprintf("10.0.%d.%d", i/256, i%256)})
		if err != nil {
			t.Fatalf("Upsert(%d) error: %v", i, err)
		}
	}
	if len(s.List()) != 100 {
		t.Errorf("List() = %d, want 100", len(s.List()))
	}
}

func TestParseSyncPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    SyncPolicy
		wantErr bool
	}{
		{name: "sync", input: "sync", want: PolicySync},
		{name: "crud alias", input: "crud", want: PolicySync},
		{name: "create-only", input: "create-only", want: PolicyCreateOnly},
		{name: "update-only", input: "update-only", want: PolicyUpdateOnly},
		{name: "upsert-only", input: "upsert-only", want: PolicyUpsertOnly},
		{name: "case insensitive", input: "CREATE-ONLY", want: PolicyCreateOnly},
		{name: "mixed case", input: "Upsert-Only", want: PolicyUpsertOnly},
		{name: "invalid value", input: "delete-only", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSyncPolicy(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSyncPolicy(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSyncPolicy(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSyncPolicy_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		policy SyncPolicy
		want   string
	}{
		{PolicySync, "sync"},
		{PolicyCreateOnly, "create-only"},
		{PolicyUpdateOnly, "update-only"},
		{PolicyUpsertOnly, "upsert-only"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.policy.String(); got != tt.want {
				t.Errorf("SyncPolicy.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStore_LoadFromTestdata(t *testing.T) {
	t.Parallel()

	// Copy testdata to temp dir to avoid modifying fixtures
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	src, err := os.ReadFile("testdata/records.json")
	if err != nil {
		t.Fatalf("ReadFile(testdata) error: %v", err)
	}
	if err := os.WriteFile(fp, src, 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	defer s.Stop()

	all := s.List()
	if len(all) != 9 {
		t.Errorf("List() returned %d records, want 9", len(all))
	}
}
