// ABOUTME: REST API server for CRUD operations on DNS records.
// ABOUTME: Uses Go 1.22+ method routing with auth middleware and JSON encoding.

package dynupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// apiListResponse wraps a list of records for JSON serialisation.
type apiListResponse struct {
	Records []Record `json:"records"`
}

// apiErrorResponse wraps an error message for JSON serialisation.
type apiErrorResponse struct {
	Error string `json:"error"`
}

// APIServer serves the REST management API.
type APIServer struct {
	store  *Store
	auth   *Auth
	listen string
	tls    *tlsConfig
	server *http.Server
}

// NewAPIServer creates an API server (not yet started).
func NewAPIServer(store *Store, auth *Auth, listen string, tls *tlsConfig) *APIServer {
	return &APIServer{store: store, auth: auth, listen: listen, tls: tls}
}

// handler builds the http.Handler with routing and middleware.
func (a *APIServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/records", a.handleList)
	mux.HandleFunc("GET /api/v1/records/{name}", a.handleGetByName)
	mux.HandleFunc("POST /api/v1/records", a.handleCreate)
	mux.HandleFunc("PUT /api/v1/records", a.handleUpdate)
	mux.HandleFunc("DELETE /api/v1/records/{name}/{type}", a.handleDeleteByType)
	mux.HandleFunc("DELETE /api/v1/records/{name}", a.handleDeleteAll)

	return a.auth.HTTPMiddleware(mux)
}

// Start begins serving the REST API in a background goroutine.
func (a *APIServer) Start() error {
	ln, err := net.Listen("tcp", a.listen)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", a.listen, err)
	}

	a.server = &http.Server{
		Handler:           a.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := a.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Errorf("API server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the API server.
func (a *APIServer) Stop() {
	if a.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.server.Shutdown(ctx)
}

func (a *APIServer) handleList(w http.ResponseWriter, r *http.Request) {
	nameFilter := r.URL.Query().Get("name")

	var records []Record
	if nameFilter != "" {
		records = a.store.GetAll(nameFilter)
	} else {
		records = a.store.List()
	}

	if records == nil {
		records = []Record{}
	}

	writeJSON(w, http.StatusOK, apiListResponse{Records: records})
}

func (a *APIServer) handleGetByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, apiErrorResponse{Error: "name is required"})
		return
	}

	records := a.store.GetAll(name)
	if records == nil {
		records = []Record{}
	}

	writeJSON(w, http.StatusOK, apiListResponse{Records: records})
}

func (a *APIServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	var rec Record
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		writeJSON(w, http.StatusBadRequest, apiErrorResponse{Error: fmt.Sprintf("invalid JSON: %v", err)})
		return
	}

	if err := rec.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, apiErrorResponse{Error: err.Error()})
		return
	}

	if err := a.store.Upsert(rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, rec)
}

func (a *APIServer) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var rec Record
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		writeJSON(w, http.StatusBadRequest, apiErrorResponse{Error: fmt.Sprintf("invalid JSON: %v", err)})
		return
	}

	if err := rec.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, apiErrorResponse{Error: err.Error()})
		return
	}

	if err := a.store.Upsert(rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, rec)
}

func (a *APIServer) handleDeleteAll(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, apiErrorResponse{Error: "name is required"})
		return
	}

	if err := a.store.DeleteAll(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *APIServer) handleDeleteByType(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	qtype := strings.ToUpper(r.PathValue("type"))

	if name == "" || qtype == "" {
		writeJSON(w, http.StatusBadRequest, apiErrorResponse{Error: "name and type are required"})
		return
	}

	// Delete all records matching name + type
	records := a.store.Get(name, qtype)
	for _, rec := range records {
		if err := a.store.Delete(name, qtype, rec.Value); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: err.Error()})
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
