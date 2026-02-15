// ABOUTME: Thread-safe in-memory record store with atomic JSON persistence.
// ABOUTME: Supports CRUD operations, auto-reload on external file changes, and concurrency safety.

package dynupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// storeFile is the JSON envelope for persisted records.
type storeFile struct {
	Records []Record `json:"records"`
}

// Store holds DNS records in memory with optional JSON file backing.
type Store struct {
	mu       sync.RWMutex
	records  map[string][]Record // key: lowercase FQDN
	filePath string
	reload   time.Duration
	lastMod  time.Time
	stopCh   chan struct{}
	ready    bool
}

// NewStore creates a store backed by the given file path.
// If the file exists, its records are loaded. If not, an empty file is created.
// A reload duration of 0 disables auto-reload.
func NewStore(filePath string, reload time.Duration) (*Store, error) {
	s := &Store{
		records:  make(map[string][]Record),
		filePath: filePath,
		reload:   reload,
		stopCh:   make(chan struct{}),
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
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(r.Name)
	recs := s.records[key]

	found := false
	for i, existing := range recs {
		if strings.EqualFold(existing.Type, r.Type) && existing.Value == r.Value {
			recs[i] = r
			found = true
			break
		}
	}
	if !found {
		recs = append(recs, r)
	}
	s.records[key] = recs

	return s.persist()
}

// Delete removes a specific record identified by name, type, and value.
func (s *Store) Delete(name, qtype, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	return s.persist()
}

// DeleteAll removes every record for the given FQDN.
func (s *Store) DeleteAll(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(name)
	delete(s.records, key)

	return s.persist()
}

// persist writes all records to the backing file atomically.
func (s *Store) persist() error {
	all := s.collectLocked()
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

	// Update lastMod to prevent self-triggered reload
	if info, err := os.Stat(s.filePath); err == nil {
		s.lastMod = info.ModTime()
	}

	return nil
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
		return s.persist()
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
	info, err := os.Stat(s.filePath)
	if err != nil {
		return
	}
	if !info.ModTime().After(s.lastMod) {
		return
	}

	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadFromBytes(raw)
}
