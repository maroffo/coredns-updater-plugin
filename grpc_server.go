// ABOUTME: gRPC server for DNS record management via protobuf.
// ABOUTME: Stub implementation; full gRPC service added in Phase 7.

package dynupdate

// GRPCServer serves the gRPC management API.
type GRPCServer struct {
	store  *Store
	auth   *Auth
	listen string
	tls    *tlsConfig
}

// NewGRPCServer creates a gRPC server (not yet started).
func NewGRPCServer(store *Store, auth *Auth, listen string, tls *tlsConfig) *GRPCServer {
	return &GRPCServer{store: store, auth: auth, listen: listen, tls: tls}
}

// Start begins serving the gRPC API.
func (g *GRPCServer) Start() error { return nil }

// Stop gracefully shuts down the gRPC server.
func (g *GRPCServer) Stop() {}
