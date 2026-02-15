// ABOUTME: REST API server for CRUD operations on DNS records.
// ABOUTME: Stub implementation; full CRUD endpoints added in Phase 6.

package dynupdate

// APIServer serves the REST management API.
type APIServer struct {
	store  *Store
	auth   *Auth
	listen string
	tls    *tlsConfig
}

// NewAPIServer creates an API server (not yet started).
func NewAPIServer(store *Store, auth *Auth, listen string, tls *tlsConfig) *APIServer {
	return &APIServer{store: store, auth: auth, listen: listen, tls: tls}
}

// Start begins serving the REST API.
func (a *APIServer) Start() error { return nil }

// Stop gracefully shuts down the API server.
func (a *APIServer) Stop() {}
