// ABOUTME: Dual-mode authentication: Bearer token + mTLS CN validation.
// ABOUTME: Provides HTTP middleware and gRPC unary interceptor for access control.

package dynupdate

// Auth holds authentication configuration for the management APIs.
type Auth struct {
	Token     string
	AllowedCN []string
}
