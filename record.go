// ABOUTME: Record data model with per-type validation and dns.RR conversion.
// ABOUTME: Supports A, AAAA, CNAME, TXT, MX, SRV, NS, PTR, CAA record types.

package dynupdate

import (
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
)

const (
	DefaultTTL = 3600
	MinTTL     = 60
	MaxTTL     = 86400
	txtChunk   = 255
)

// supportedTypes enumerates DNS record types this plugin can manage.
var supportedTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "TXT": true,
	"MX": true, "SRV": true, "NS": true, "PTR": true, "CAA": true,
}

// validCAATags enumerates the allowed CAA tag values.
var validCAATags = map[string]bool{
	"issue": true, "issuewild": true, "iodef": true,
}

// Record represents a single DNS record managed by the dynupdate plugin.
type Record struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	TTL      uint32 `json:"ttl"`
	Value    string `json:"value"`
	Priority uint16 `json:"priority,omitempty"`
	Weight   uint16 `json:"weight,omitempty"`
	Port     uint16 `json:"port,omitempty"`
	Flag     uint8  `json:"flag,omitempty"`
	Tag      string `json:"tag,omitempty"`
}

// Validate checks the record fields for correctness.
// It normalises Type to uppercase and sets a default TTL when zero.
func (r *Record) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if !strings.HasSuffix(r.Name, ".") {
		return fmt.Errorf("name %q must end with a trailing dot", r.Name)
	}

	r.Type = strings.ToUpper(r.Type)
	if r.Type == "" {
		return fmt.Errorf("type must not be empty")
	}
	if !supportedTypes[r.Type] {
		return fmt.Errorf("unsupported record type %q", r.Type)
	}

	if r.TTL == 0 {
		r.TTL = DefaultTTL
	}
	if r.TTL < MinTTL || r.TTL > MaxTTL {
		return fmt.Errorf("TTL %d out of range [%d, %d]", r.TTL, MinTTL, MaxTTL)
	}

	return r.validateValue()
}

func (r *Record) validateValue() error {
	switch r.Type {
	case "A":
		return r.validateA()
	case "AAAA":
		return r.validateAAAA()
	case "CNAME", "NS", "PTR":
		return r.validateFQDN()
	case "TXT":
		return r.validateTXT()
	case "MX":
		return r.validateMX()
	case "SRV":
		return r.validateSRV()
	case "CAA":
		return r.validateCAA()
	}
	return nil
}

func (r *Record) validateA() error {
	ip := net.ParseIP(r.Value)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("value %q is not a valid IPv4 address", r.Value)
	}
	return nil
}

func (r *Record) validateAAAA() error {
	ip := net.ParseIP(r.Value)
	if ip == nil || ip.To4() != nil {
		return fmt.Errorf("value %q is not a valid IPv6 address", r.Value)
	}
	return nil
}

func (r *Record) validateFQDN() error {
	if !dns.IsFqdn(r.Value) {
		return fmt.Errorf("value %q must be a FQDN with trailing dot", r.Value)
	}
	return nil
}

func (r *Record) validateTXT() error {
	if r.Value == "" {
		return fmt.Errorf("TXT value must not be empty")
	}
	return nil
}

func (r *Record) validateMX() error {
	if !dns.IsFqdn(r.Value) {
		return fmt.Errorf("MX value %q must be a FQDN with trailing dot", r.Value)
	}
	return nil
}

func (r *Record) validateSRV() error {
	if !dns.IsFqdn(r.Value) {
		return fmt.Errorf("SRV target %q must be a FQDN with trailing dot", r.Value)
	}
	if r.Port == 0 {
		return fmt.Errorf("SRV port must be non-zero")
	}
	return nil
}

func (r *Record) validateCAA() error {
	if r.Value == "" {
		return fmt.Errorf("CAA value must not be empty")
	}
	if r.Tag == "" {
		return fmt.Errorf("CAA tag must not be empty")
	}
	if !validCAATags[r.Tag] {
		return fmt.Errorf("CAA tag %q is invalid; must be one of: issue, issuewild, iodef", r.Tag)
	}
	return nil
}

// ToRR converts a Record into a miekg/dns RR. The record should be validated
// before calling this method.
func (r Record) ToRR() (dns.RR, error) {
	hdr := dns.RR_Header{
		Name:   r.Name,
		Rrtype: dns.StringToType[r.Type],
		Class:  dns.ClassINET,
		Ttl:    r.TTL,
	}

	if hdr.Rrtype == 0 {
		return nil, fmt.Errorf("unsupported record type %q", r.Type)
	}

	switch r.Type {
	case "A":
		return &dns.A{Hdr: hdr, A: net.ParseIP(r.Value).To4()}, nil
	case "AAAA":
		return &dns.AAAA{Hdr: hdr, AAAA: net.ParseIP(r.Value)}, nil
	case "CNAME":
		return &dns.CNAME{Hdr: hdr, Target: r.Value}, nil
	case "TXT":
		return &dns.TXT{Hdr: hdr, Txt: splitTXT(r.Value)}, nil
	case "MX":
		return &dns.MX{Hdr: hdr, Preference: r.Priority, Mx: r.Value}, nil
	case "SRV":
		return &dns.SRV{Hdr: hdr, Priority: r.Priority, Weight: r.Weight, Port: r.Port, Target: r.Value}, nil
	case "NS":
		return &dns.NS{Hdr: hdr, Ns: r.Value}, nil
	case "PTR":
		return &dns.PTR{Hdr: hdr, Ptr: r.Value}, nil
	case "CAA":
		return &dns.CAA{Hdr: hdr, Flag: r.Flag, Tag: r.Tag, Value: r.Value}, nil
	default:
		return nil, fmt.Errorf("unsupported record type %q", r.Type)
	}
}

// splitTXT breaks a TXT value into 255-byte chunks as required by RFC 4408.
func splitTXT(s string) []string {
	if len(s) <= txtChunk {
		return []string{s}
	}
	var chunks []string
	for len(s) > 0 {
		end := txtChunk
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[:end])
		s = s[end:]
	}
	return chunks
}
