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
		}
	}`

	c := caddy.NewTestController("dns", input)
	err := setup(c)
	if err != nil {
		t.Fatalf("setup() error: %v", err)
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
