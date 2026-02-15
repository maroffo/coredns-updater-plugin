// ABOUTME: Corefile parser and plugin registration for dynupdate.
// ABOUTME: Handles plugin.Register, Corefile block parsing, and OnStartup/OnShutdown lifecycle.

package dynupdate

import (
	"fmt"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register(pluginName, setup) }

// pluginConfig holds parsed Corefile configuration.
type pluginConfig struct {
	zones    []string
	datafile string
	reload   time.Duration

	apiListen string
	apiToken  string
	apiTLS    *tlsConfig

	grpcListen string
	grpcToken  string
	grpcTLS    *tlsConfig

	apiAllowedCN  []string
	apiNoAuth     bool

	grpcAllowedCN []string
	grpcNoAuth    bool

	maxRecords int
	syncPolicy SyncPolicy
	fallArgs   []string
}

type tlsConfig struct {
	cert string
	key  string
	ca   string
}

func setup(c *caddy.Controller) error {
	cfg, err := parseConfig(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	var storeOpts []StoreOption
	if cfg.maxRecords > 0 {
		storeOpts = append(storeOpts, WithMaxRecords(cfg.maxRecords))
	}
	if cfg.syncPolicy != PolicySync {
		storeOpts = append(storeOpts, WithSyncPolicy(cfg.syncPolicy))
	}

	store, err := NewStore(cfg.datafile, cfg.reload, storeOpts...)
	if err != nil {
		return plugin.Error(pluginName, fmt.Errorf("creating store: %w", err))
	}

	d := &DynUpdate{
		Zones: cfg.zones,
		Store: store,
	}

	if cfg.fallArgs != nil {
		d.Fall.SetZonesFromArgs(cfg.fallArgs)
	}

	// Start API server if configured
	var apiSrv *APIServer
	if cfg.apiListen != "" {
		auth := &Auth{Token: cfg.apiToken, AllowedCN: cfg.apiAllowedCN, NoAuth: cfg.apiNoAuth}
		apiSrv = NewAPIServer(store, auth, cfg.apiListen, cfg.apiTLS)
	}

	// Start gRPC server if configured
	var grpcSrv *GRPCServer
	if cfg.grpcListen != "" {
		auth := &Auth{Token: cfg.grpcToken, AllowedCN: cfg.grpcAllowedCN, NoAuth: cfg.grpcNoAuth}
		grpcSrv = NewGRPCServer(store, auth, cfg.grpcListen, cfg.grpcTLS)
	}

	c.OnStartup(func() error {
		if apiSrv != nil {
			if err := apiSrv.Start(); err != nil {
				return fmt.Errorf("starting API server: %w", err)
			}
			log.Infof("REST API listening on %s", cfg.apiListen)
		}
		if grpcSrv != nil {
			if err := grpcSrv.Start(); err != nil {
				return fmt.Errorf("starting gRPC server: %w", err)
			}
			log.Infof("gRPC server listening on %s", cfg.grpcListen)
		}
		return nil
	})

	c.OnShutdown(func() error {
		store.Stop()
		if apiSrv != nil {
			apiSrv.Stop()
		}
		if grpcSrv != nil {
			grpcSrv.Stop()
		}
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		d.Next = next
		return d
	})

	return nil
}

func parseConfig(c *caddy.Controller) (*pluginConfig, error) {
	cfg := &pluginConfig{}

	c.Next() // skip "dynupdate"

	// Parse zone arguments
	cfg.zones = c.RemainingArgs()
	if len(cfg.zones) == 0 {
		cfg.zones = make([]string, len(c.ServerBlockKeys))
		copy(cfg.zones, c.ServerBlockKeys)
	}

	// Normalise zones to FQDN
	var normalized []string
	for _, z := range cfg.zones {
		normalized = append(normalized, plugin.Host(z).NormalizeExact()...)
	}
	cfg.zones = normalized

	for c.NextBlock() {
		switch c.Val() {
		case "datafile":
			if !c.NextArg() {
				return nil, fmt.Errorf("datafile requires a path argument")
			}
			cfg.datafile = c.Val()

		case "reload":
			if !c.NextArg() {
				return nil, fmt.Errorf("reload requires a duration argument")
			}
			d, err := time.ParseDuration(c.Val())
			if err != nil {
				return nil, fmt.Errorf("invalid reload duration %q: %w", c.Val(), err)
			}
			cfg.reload = d

		case "api":
			if err := parseNestedBlock(c, func(key string, c *caddy.Controller) error {
				return parseAPIDirective(key, c, cfg)
			}); err != nil {
				return nil, err
			}

		case "grpc":
			if err := parseNestedBlock(c, func(key string, c *caddy.Controller) error {
				return parseGRPCDirective(key, c, cfg)
			}); err != nil {
				return nil, err
			}

		case "max_records":
			if !c.NextArg() {
				return nil, fmt.Errorf("max_records requires a numeric argument")
			}
			n, err := strconv.Atoi(c.Val())
			if err != nil || n < 0 {
				return nil, fmt.Errorf("max_records must be a non-negative integer: %q", c.Val())
			}
			cfg.maxRecords = n

		case "sync_policy":
			if !c.NextArg() {
				return nil, fmt.Errorf("sync_policy requires an argument")
			}
			p, err := ParseSyncPolicy(c.Val())
			if err != nil {
				return nil, fmt.Errorf("invalid sync_policy: %w", err)
			}
			cfg.syncPolicy = p

		case "fallthrough":
			cfg.fallArgs = c.RemainingArgs()

		default:
			return nil, fmt.Errorf("unknown directive %q", c.Val())
		}
	}

	if cfg.datafile == "" {
		return nil, fmt.Errorf("datafile is required")
	}

	if cfg.apiListen != "" && cfg.apiToken == "" && len(cfg.apiAllowedCN) == 0 && !cfg.apiNoAuth {
		return nil, fmt.Errorf("api block requires token, allowed_cn, or explicit no_auth directive")
	}
	if cfg.grpcListen != "" && cfg.grpcToken == "" && len(cfg.grpcAllowedCN) == 0 && !cfg.grpcNoAuth {
		return nil, fmt.Errorf("grpc block requires token, allowed_cn, or explicit no_auth directive")
	}

	return cfg, nil
}

// parseNestedBlock manually handles Caddy v1 nested block parsing.
// It consumes the opening `{`, iterates over directives, and stops at `}`.
func parseNestedBlock(c *caddy.Controller, handler func(string, *caddy.Controller) error) error {
	// Expect the opening brace on the same or next line
	if !c.Next() {
		return nil // empty block without braces is OK
	}
	if c.Val() != "{" {
		// Not a block; treat as a single-line directive
		return handler(c.Val(), c)
	}

	for c.Next() {
		if c.Val() == "}" {
			return nil
		}
		if err := handler(c.Val(), c); err != nil {
			return err
		}
	}
	return nil
}

func parseAPIDirective(key string, c *caddy.Controller, cfg *pluginConfig) error {
	switch key {
	case "listen":
		if !c.NextArg() {
			return fmt.Errorf("api listen requires an address")
		}
		cfg.apiListen = c.Val()

	case "token":
		if !c.NextArg() {
			return fmt.Errorf("api token requires a value")
		}
		cfg.apiToken = c.Val()

	case "tls":
		args := c.RemainingArgs()
		if len(args) != 3 {
			return fmt.Errorf("api tls requires CERT KEY CA arguments")
		}
		cfg.apiTLS = &tlsConfig{cert: args[0], key: args[1], ca: args[2]}

	case "allowed_cn":
		cfg.apiAllowedCN = c.RemainingArgs()
		if len(cfg.apiAllowedCN) == 0 {
			return fmt.Errorf("allowed_cn requires at least one CN")
		}

	case "no_auth":
		cfg.apiNoAuth = true

	default:
		return fmt.Errorf("unknown api directive %q", key)
	}
	return nil
}

func parseGRPCDirective(key string, c *caddy.Controller, cfg *pluginConfig) error {
	switch key {
	case "listen":
		if !c.NextArg() {
			return fmt.Errorf("grpc listen requires an address")
		}
		cfg.grpcListen = c.Val()

	case "token":
		if !c.NextArg() {
			return fmt.Errorf("grpc token requires a value")
		}
		cfg.grpcToken = c.Val()

	case "tls":
		args := c.RemainingArgs()
		if len(args) != 3 {
			return fmt.Errorf("grpc tls requires CERT KEY CA arguments")
		}
		cfg.grpcTLS = &tlsConfig{cert: args[0], key: args[1], ca: args[2]}

	case "allowed_cn":
		cfg.grpcAllowedCN = c.RemainingArgs()
		if len(cfg.grpcAllowedCN) == 0 {
			return fmt.Errorf("allowed_cn requires at least one CN")
		}

	case "no_auth":
		cfg.grpcNoAuth = true

	default:
		return fmt.Errorf("unknown grpc directive %q", key)
	}
	return nil
}
