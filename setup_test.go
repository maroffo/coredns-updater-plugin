// ABOUTME: Tests for Corefile parsing and plugin setup.
// ABOUTME: Covers valid configurations, missing directives, and invalid values.

package dynupdate

import (
	"testing"

	"github.com/coredns/caddy"
)

func TestSetup_ValidMinimal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err != nil {
		t.Fatalf("setup() error: %v", err)
	}
}

func TestSetup_ValidWithAPI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
		reload 30s

		api {
			listen :18080
			token super-secret
		}

		fallthrough
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err != nil {
		t.Fatalf("setup() error: %v", err)
	}
}

func TestSetup_MissingDatafile(t *testing.T) {
	t.Parallel()
	input := `dynupdate example.org. {
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err == nil {
		t.Fatal("setup() expected error for missing datafile")
	}
}

func TestSetup_InvalidReloadDuration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
		reload not-a-duration
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err == nil {
		t.Fatal("setup() expected error for invalid reload duration")
	}
}

func TestSetup_DefaultZones(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// No zones specified; should inherit from server block
	input := `dynupdate {
		datafile ` + dir + `/records.json
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err != nil {
		t.Fatalf("setup() error: %v", err)
	}
}

func TestSetup_ValidWithGRPC(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json

		grpc {
			listen :18443
			token grpc-secret
		}
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err != nil {
		t.Fatalf("setup() error: %v", err)
	}
}

func TestSetup_FailsWithoutAuth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json

		api {
			listen :18080
		}
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err == nil {
		t.Fatal("setup() expected error when api listen set without auth")
	}
}

func TestSetup_AllowsNoAuth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json

		api {
			listen :18080
			no_auth
		}
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err != nil {
		t.Fatalf("setup() error: %v", err)
	}
}

func TestSetup_SeparateAllowedCN(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json

		api {
			listen :18080
			token api-secret
			allowed_cn api-client.example.org
		}

		grpc {
			listen :18443
			token grpc-secret
			allowed_cn grpc-client.example.org
		}
	}`

	c := caddy.NewTestController("dns", input)
	cfg, err := parseConfig(c)
	if err != nil {
		t.Fatalf("parseConfig() error: %v", err)
	}

	if len(cfg.apiAllowedCN) != 1 || cfg.apiAllowedCN[0] != "api-client.example.org" {
		t.Errorf("apiAllowedCN = %v, want [api-client.example.org]", cfg.apiAllowedCN)
	}
	if len(cfg.grpcAllowedCN) != 1 || cfg.grpcAllowedCN[0] != "grpc-client.example.org" {
		t.Errorf("grpcAllowedCN = %v, want [grpc-client.example.org]", cfg.grpcAllowedCN)
	}
}

func TestSetup_SyncPolicyValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
		sync_policy create-only
	}`

	c := caddy.NewTestController("dns", input)
	cfg, err := parseConfig(c)
	if err != nil {
		t.Fatalf("parseConfig() error: %v", err)
	}
	if cfg.syncPolicy != PolicyCreateOnly {
		t.Errorf("syncPolicy = %v, want %v", cfg.syncPolicy, PolicyCreateOnly)
	}
}

func TestSetup_SyncPolicySync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
		sync_policy sync
	}`

	c := caddy.NewTestController("dns", input)
	cfg, err := parseConfig(c)
	if err != nil {
		t.Fatalf("parseConfig() error: %v", err)
	}
	if cfg.syncPolicy != PolicySync {
		t.Errorf("syncPolicy = %v, want %v", cfg.syncPolicy, PolicySync)
	}
}

func TestSetup_SyncPolicyMissingArg(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
		sync_policy
	}`

	c := caddy.NewTestController("dns", input)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("parseConfig() expected error for missing sync_policy argument")
	}
}

func TestSetup_SyncPolicyInvalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
		sync_policy delete-only
	}`

	c := caddy.NewTestController("dns", input)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("parseConfig() expected error for invalid sync_policy value")
	}
}

func TestSetup_SyncPolicyOmittedDefaultsToSync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. {
		datafile ` + dir + `/records.json
	}`

	c := caddy.NewTestController("dns", input)
	cfg, err := parseConfig(c)
	if err != nil {
		t.Fatalf("parseConfig() error: %v", err)
	}
	if cfg.syncPolicy != PolicySync {
		t.Errorf("syncPolicy = %v, want %v (default)", cfg.syncPolicy, PolicySync)
	}
}

func TestSetup_FallthroughWithZones(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := `dynupdate example.org. example.net. {
		datafile ` + dir + `/records.json
		fallthrough example.org.
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err != nil {
		t.Fatalf("setup() error: %v", err)
	}
}
