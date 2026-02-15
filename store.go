// ABOUTME: Thread-safe in-memory record store with atomic JSON persistence.
// ABOUTME: Supports CRUD operations, auto-reload on external file changes, and concurrency safety.

package dynupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrPolicyDenied is returned when a mutation is rejected by the sync policy.
var ErrPolicyDenied = errors.New("operation denied by sync policy")

// SyncPolicy controls which mutation operations the store permits.
type SyncPolicy uint8

const (
	// PolicySync allows full CRUD operations (default zero-value).
	PolicySync SyncPolicy = iota
	// PolicyCreateOnly allows only creating new records.
	PolicyCreateOnly
	// PolicyUpdateOnly allows only updating existing records.
	PolicyUpdateOnly
	// PolicyUpsertOnly allows creating and updating, but not deleting.
	PolicyUpsertOnly
)

// ParseSyncPolicy parses a string into a SyncPolicy.
// Valid values: "sync", "crud", "create-only", "update-only", "upsert-only".
func ParseSyncPolicy(s string) (SyncPolicy, error) {
	switch strings.ToLower(s) {
	case "sync", "crud":
		return PolicySync, nil
	case "create-only":
		return PolicyCreateOnly, nil
	case "update-only":
		return PolicyUpdateOnly, nil
	case "upsert-only":
		return PolicyUpsertOnly, nil
	default:
		return 0, fmt.Errorf("unknown sync policy %q: valid values are sync, crud, create-only, update-only, upsert-only", s)
	}
}

// String returns the canonical string representation of the policy.
func (p SyncPolicy) String() string {
	switch p {
	case PolicySync:
		return "sync"
	case PolicyCreateOnly:
		return "create-only"
	case PolicyUpdateOnly:
		return "update-only"
	case PolicyUpsertOnly:
		return "upsert-only"
	default:
		return fmt.Sprintf("SyncPolicy(%d)", p)
	}
}

// storeFile is the JSON envelope for persisted records.
type storeFile struct {
	Records []Record `json:"records"`
}

// Store holds DNS records in memory with optional JSON file backing.
type Store struct {
	mu         sync.RWMutex
	records    map[string][]Record // key: lowercase FQDN
	filePath   string
	reload     time.Duration
	lastMod    time.Time
	stopCh     chan struct{}
	ready      bool
	maxRecords int
	syncPolicy SyncPolicy
	persistMu  sync.Mutex // serializes file writes, independent of mu
	generation uint64     // incremented on each mutation (under mu)
	persisted  uint64     // generation of last successful persist (under persistMu, updated under mu)
}

// StoreOption configures optional Store behaviour.
type StoreOption func(*Store)

// WithMaxRecords sets the maximum number of records the store will hold.
// A value of 0 (default) means unlimited.
func WithMaxRecords(n int) StoreOption {
	return func(s *Store) {
		s.maxRecords = n
	}
}

// WithSyncPolicy sets the mutation policy for the store.
func WithSyncPolicy(p SyncPolicy) StoreOption {
	return func(s *Store) {
		s.syncPolicy = p
	}
}

// NewStore creates a store backed by the given file path.
// If the file exists, its records are loaded. If not, an empty file is created.
// A reload duration of 0 disables auto-reload.
func NewStore(filePath string, reload time.Duration, opts ...StoreOption) (*Store, error) {
	s := &Store{
		records:  make(map[string][]Record),
		filePath: filePath,
		reload:   reload,
		stopCh:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(s)
	}

	if err := s.loadOrCreate(); err != nil {
		return nil, fmt.Errorf("initialising store from %s: %w", filePath, err)
	}

	s.ready = true

	if reload > 0 {
		go s.run()
	}
	return s, nil
}

// Ready reports whether the store has completed initial loading.
func (s *Store) Ready() bool {
	return s.ready
}

// Stop terminates the auto-reload goroutine.
func (s *Store) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

// Get returns records matching the given FQDN and record type.
func (s *Store) Get(name, qtype string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := strings.ToLower(name)
	var result []Record
	for _, r := range s.records[key] {
		if strings.EqualFold(r.Type, qtype) {
			result = append(result, r)
		}
	}
	return result
}

// GetAll returns all records for the given FQDN regardless of type.
func (s *Store) GetAll(name string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := strings.ToLower(name)
	recs := s.records[key]
	out := make([]Record, len(recs))
	copy(out, recs)
	return out
}

// List returns every record in the store as a flat slice.
func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []Record
	for _, recs := range s.records {
		all = append(all, recs...)
	}
	return all
}

// Upsert adds or updates a record. Matching is done on name+type+value.
// The file is persisted atomically after the operation.
func (s *Store) Upsert(r Record) error {
	snapshot, gen, err := s.applyUpsert(r)
	if err != nil {
		return err
	}
	return s.persistSnapshot(snapshot, gen)
}

func (s *Store) applyUpsert(r Record) ([]Record, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(r.Name)
	recs := s.records[key]

	idx := -1
	for i, existing := range recs {
		if strings.EqualFold(existing.Type, r.Type) && existing.Value == r.Value {
			idx = i
			break
		}
	}
	found := idx >= 0

	// Policy check before mutation
	switch {
	case s.syncPolicy == PolicyCreateOnly && found:
		return nil, 0, fmt.Errorf("cannot update record %s (type %s): %w", r.Name, r.Type, ErrPolicyDenied)
	case s.syncPolicy == PolicyUpdateOnly && !found:
		return nil, 0, fmt.Errorf("cannot create record %s (type %s): %w", r.Name, r.Type, ErrPolicyDenied)
	}

	if found {
		recs[idx] = r
	} else {
		if s.maxRecords > 0 && s.countLocked() >= s.maxRecords {
			return nil, 0, fmt.Errorf("record limit of %d reached", s.maxRecords)
		}
		recs = append(recs, r)
	}
	s.records[key] = recs

	s.generation++
	return s.collectLocked(), s.generation, nil
}

// Delete removes a specific record identified by name, type, and value.
func (s *Store) Delete(name, qtype, value string) error {
	snapshot, gen, err := s.applyDelete(name, qtype, value)
	if err != nil {
		return err
	}
	return s.persistSnapshot(snapshot, gen)
}

func (s *Store) applyDelete(name, qtype, value string) ([]Record, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.syncPolicy != PolicySync {
		return nil, 0, fmt.Errorf("delete denied: %w", ErrPolicyDenied)
	}

	key := strings.ToLower(name)
	recs := s.records[key]
	filtered := recs[:0]
	for _, r := range recs {
		if strings.EqualFold(r.Type, qtype) && r.Value == value {
			continue
		}
		filtered = append(filtered, r)
	}

	if len(filtered) == 0 {
		delete(s.records, key)
	} else {
		s.records[key] = filtered
	}

	s.generation++
	return s.collectLocked(), s.generation, nil
}

// DeleteByType removes all records matching the given FQDN and record type
// in a single atomic operation (one lock, one persist).
func (s *Store) DeleteByType(name, qtype string) error {
	snapshot, gen, err := s.applyDeleteByType(name, qtype)
	if err != nil {
		return err
	}
	return s.persistSnapshot(snapshot, gen)
}

func (s *Store) applyDeleteByType(name, qtype string) ([]Record, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.syncPolicy != PolicySync {
		return nil, 0, fmt.Errorf("delete denied: %w", ErrPolicyDenied)
	}

	key := strings.ToLower(name)
	recs := s.records[key]
	filtered := make([]Record, 0, len(recs))
	for _, r := range recs {
		if !strings.EqualFold(r.Type, qtype) {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) == 0 {
		delete(s.records, key)
	} else {
		s.records[key] = filtered
	}

	s.generation++
	return s.collectLocked(), s.generation, nil
}

// DeleteAll removes every record for the given FQDN.
func (s *Store) DeleteAll(name string) error {
	snapshot, gen, err := s.applyDeleteAll(name)
	if err != nil {
		return err
	}
	return s.persistSnapshot(snapshot, gen)
}

func (s *Store) applyDeleteAll(name string) ([]Record, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.syncPolicy != PolicySync {
		return nil, 0, fmt.Errorf("delete denied: %w", ErrPolicyDenied)
	}

	key := strings.ToLower(name)
	delete(s.records, key)

	s.generation++
	return s.collectLocked(), s.generation, nil
}

// persistSnapshot writes the given records to the backing file atomically.
// Serialized by persistMu; skips if a newer generation was already persisted.
// Must NOT be called with s.mu held.
func (s *Store) persistSnapshot(all []Record, gen uint64) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	// A newer snapshot was already written; this one is stale.
	// Safe without mu: persistMu serializes all callers, so s.persisted cannot change concurrently.
	if gen > 0 && gen <= s.persisted {
		return nil
	}

	data := storeFile{Records: all}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling store: %w", err)
	}

	dir := filepath.Dir(s.filePath)
	tmp, err := os.CreateTemp(dir, "dynupdate-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, s.filePath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp to %s: %w", s.filePath, err)
	}

	// Update metadata under mu to prevent self-triggered reload.
	s.mu.Lock()
	s.persisted = gen
	if info, err := os.Stat(s.filePath); err == nil {
		s.lastMod = info.ModTime()
	}
	s.updateRecordGaugeLocked()
	s.mu.Unlock()

	return nil
}

// updateRecordGaugeLocked sets the storeRecordGauge per record type. Caller must hold at least RLock.
func (s *Store) updateRecordGaugeLocked() {
	counts := make(map[string]float64)
	for _, recs := range s.records {
		for _, r := range recs {
			counts[r.Type]++
		}
	}
	storeRecordGauge.Reset()
	for t, c := range counts {
		storeRecordGauge.WithLabelValues(t).Set(c)
	}
}

// countLocked returns the total number of records. Caller must hold at least RLock.
func (s *Store) countLocked() int {
	n := 0
	for _, recs := range s.records {
		n += len(recs)
	}
	return n
}

// collectLocked returns all records as a flat slice. Caller must hold at least RLock.
func (s *Store) collectLocked() []Record {
	var all []Record
	for _, recs := range s.records {
		all = append(all, recs...)
	}
	return all
}

// loadOrCreate loads records from file or creates an empty file.
func (s *Store) loadOrCreate() error {
	raw, err := os.ReadFile(s.filePath)
	if os.IsNotExist(err) {
		// Create empty file
		s.records = make(map[string][]Record)
		return s.persistSnapshot(nil, 0)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.filePath, err)
	}

	return s.loadFromBytes(raw)
}

func (s *Store) loadFromBytes(raw []byte) error {
	var data storeFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	records := make(map[string][]Record)
	for _, r := range data.Records {
		key := strings.ToLower(r.Name)
		records[key] = append(records[key], r)
	}
	s.records = records

	if info, err := os.Stat(s.filePath); err == nil {
		s.lastMod = info.ModTime()
	}

	return nil
}

// run is the auto-reload goroutine that checks file mtime periodically.
func (s *Store) run() {
	ticker := time.NewTicker(s.reload)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkReload()
		}
	}
}

func (s *Store) checkReload() {
	// Skip if a persist is actively running to avoid overwriting in-flight mutations.
	if !s.persistMu.TryLock() {
		return
	}
	s.persistMu.Unlock()

	// Phase 1: check mtime under lock (fast path).
	s.mu.RLock()
	if s.generation > s.persisted {
		s.mu.RUnlock()
		return
	}
	lastMod := s.lastMod
	s.mu.RUnlock()

	info, err := os.Stat(s.filePath)
	if err != nil {
		return
	}
	if !info.ModTime().After(lastMod) {
		return
	}

	// Phase 2: read file outside any lock.
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		log.Errorf("reload %s: read error: %v", s.filePath, err)
		return
	}

	// Phase 3: re-verify under write lock and swap.
	s.mu.Lock()
	defer s.mu.Unlock()

	// A mutation may have landed while we were reading; skip if so.
	if s.generation > s.persisted {
		return
	}
	// Re-check mtime: another reload or persist may have updated lastMod.
	if !info.ModTime().After(s.lastMod) {
		return
	}

	if err := s.loadFromBytes(raw); err != nil {
		log.Errorf("reload %s: parse error: %v", s.filePath, err)
	}
}
