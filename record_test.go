// ABOUTME: Tests for the Record data model: validation per type and ToRR conversion.
// ABOUTME: Covers A, AAAA, CNAME, TXT, MX, SRV, NS, PTR, CAA record types.

package dynupdate

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestRecord_Validate_ValidRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record Record
	}{
		{
			name:   "valid A record",
			record: Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
		},
		{
			name:   "valid AAAA record",
			record: Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"},
		},
		{
			name:   "valid CNAME record",
			record: Record{Name: "alias.example.org.", Type: "CNAME", TTL: 300, Value: "app.example.org."},
		},
		{
			name:   "valid TXT record",
			record: Record{Name: "app.example.org.", Type: "TXT", TTL: 300, Value: "v=spf1 include:example.org ~all"},
		},
		{
			name:   "valid MX record",
			record: Record{Name: "example.org.", Type: "MX", TTL: 3600, Value: "mx1.example.org.", Priority: 10},
		},
		{
			name:   "valid SRV record",
			record: Record{Name: "_sip._tcp.example.org.", Type: "SRV", TTL: 3600, Value: "sip.example.org.", Priority: 10, Weight: 60, Port: 5060},
		},
		{
			name:   "valid NS record",
			record: Record{Name: "example.org.", Type: "NS", TTL: 3600, Value: "ns1.example.org."},
		},
		{
			name:   "valid PTR record",
			record: Record{Name: "1.0.0.10.in-addr.arpa.", Type: "PTR", TTL: 3600, Value: "app.example.org."},
		},
		{
			name:   "valid CAA record",
			record: Record{Name: "example.org.", Type: "CAA", TTL: 3600, Value: "letsencrypt.org", Flag: 0, Tag: "issue"},
		},
		{
			name:   "type case insensitive",
			record: Record{Name: "app.example.org.", Type: "a", TTL: 300, Value: "10.0.0.1"},
		},
		{
			name:   "zero TTL gets default",
			record: Record{Name: "app.example.org.", Type: "A", TTL: 0, Value: "10.0.0.1"},
		},
		{
			name:   "min TTL boundary",
			record: Record{Name: "app.example.org.", Type: "A", TTL: 60, Value: "10.0.0.1"},
		},
		{
			name:   "max TTL boundary",
			record: Record{Name: "app.example.org.", Type: "A", TTL: 86400, Value: "10.0.0.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.record.Validate(); err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestRecord_Validate_InvalidRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		record  Record
		wantErr string
	}{
		{
			name:    "empty name",
			record:  Record{Name: "", Type: "A", TTL: 300, Value: "10.0.0.1"},
			wantErr: "name",
		},
		{
			name:    "name without trailing dot",
			record:  Record{Name: "app.example.org", Type: "A", TTL: 300, Value: "10.0.0.1"},
			wantErr: "trailing dot",
		},
		{
			name:    "empty type",
			record:  Record{Name: "app.example.org.", Type: "", TTL: 300, Value: "10.0.0.1"},
			wantErr: "type",
		},
		{
			name:    "unsupported type",
			record:  Record{Name: "app.example.org.", Type: "HINFO", TTL: 300, Value: "some"},
			wantErr: "unsupported",
		},
		{
			name:    "TTL below minimum",
			record:  Record{Name: "app.example.org.", Type: "A", TTL: 59, Value: "10.0.0.1"},
			wantErr: "TTL",
		},
		{
			name:    "TTL above maximum",
			record:  Record{Name: "app.example.org.", Type: "A", TTL: 86401, Value: "10.0.0.1"},
			wantErr: "TTL",
		},
		{
			name:    "A record with IPv6",
			record:  Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "2001:db8::1"},
			wantErr: "IPv4",
		},
		{
			name:    "A record with invalid IP",
			record:  Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "not-an-ip"},
			wantErr: "IPv4",
		},
		{
			name:    "AAAA record with IPv4",
			record:  Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "10.0.0.1"},
			wantErr: "IPv6",
		},
		{
			name:    "CNAME without trailing dot",
			record:  Record{Name: "alias.example.org.", Type: "CNAME", TTL: 300, Value: "app.example.org"},
			wantErr: "FQDN",
		},
		{
			name:    "TXT empty value",
			record:  Record{Name: "app.example.org.", Type: "TXT", TTL: 300, Value: ""},
			wantErr: "empty",
		},
		{
			name:    "MX without trailing dot on value",
			record:  Record{Name: "example.org.", Type: "MX", TTL: 300, Value: "mx1.example.org", Priority: 10},
			wantErr: "FQDN",
		},
		{
			name:    "SRV missing port",
			record:  Record{Name: "_sip._tcp.example.org.", Type: "SRV", TTL: 300, Value: "sip.example.org.", Priority: 10, Weight: 60, Port: 0},
			wantErr: "port",
		},
		{
			name:    "NS without trailing dot on value",
			record:  Record{Name: "example.org.", Type: "NS", TTL: 300, Value: "ns1.example.org"},
			wantErr: "FQDN",
		},
		{
			name:    "PTR without trailing dot on value",
			record:  Record{Name: "1.0.0.10.in-addr.arpa.", Type: "PTR", TTL: 300, Value: "app.example.org"},
			wantErr: "FQDN",
		},
		{
			name:    "CAA empty value",
			record:  Record{Name: "example.org.", Type: "CAA", TTL: 300, Value: "", Tag: "issue"},
			wantErr: "empty",
		},
		{
			name:    "CAA empty tag",
			record:  Record{Name: "example.org.", Type: "CAA", TTL: 300, Value: "letsencrypt.org", Tag: ""},
			wantErr: "tag",
		},
		{
			name:    "CAA invalid tag",
			record:  Record{Name: "example.org.", Type: "CAA", TTL: 300, Value: "letsencrypt.org", Tag: "badtag"},
			wantErr: "tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.record.Validate()
			if err == nil {
				t.Fatal("Validate() expected error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
				t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRecord_Validate_DefaultTTL(t *testing.T) {
	t.Parallel()
	r := Record{Name: "app.example.org.", Type: "A", TTL: 0, Value: "10.0.0.1"}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	if r.TTL != DefaultTTL {
		t.Errorf("TTL = %d, want %d", r.TTL, DefaultTTL)
	}
}

func TestRecord_Validate_NormalizesType(t *testing.T) {
	t.Parallel()
	r := Record{Name: "app.example.org.", Type: "aaaa", TTL: 300, Value: "2001:db8::1"}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	if r.Type != "AAAA" {
		t.Errorf("Type = %q, want %q", r.Type, "AAAA")
	}
}

func TestRecord_ToRR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		record   Record
		wantType uint16
		check    func(t *testing.T, rr dns.RR)
	}{
		{
			name:     "A record",
			record:   Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
			wantType: dns.TypeA,
			check: func(t *testing.T, rr dns.RR) {
				a := rr.(*dns.A)
				if a.A.String() != "10.0.0.1" {
					t.Errorf("A.A = %s, want 10.0.0.1", a.A)
				}
			},
		},
		{
			name:     "AAAA record",
			record:   Record{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"},
			wantType: dns.TypeAAAA,
			check: func(t *testing.T, rr dns.RR) {
				aaaa := rr.(*dns.AAAA)
				if aaaa.AAAA.String() != "2001:db8::1" {
					t.Errorf("AAAA.AAAA = %s, want 2001:db8::1", aaaa.AAAA)
				}
			},
		},
		{
			name:     "CNAME record",
			record:   Record{Name: "alias.example.org.", Type: "CNAME", TTL: 300, Value: "app.example.org."},
			wantType: dns.TypeCNAME,
			check: func(t *testing.T, rr dns.RR) {
				cname := rr.(*dns.CNAME)
				if cname.Target != "app.example.org." {
					t.Errorf("CNAME.Target = %s, want app.example.org.", cname.Target)
				}
			},
		},
		{
			name:     "TXT record",
			record:   Record{Name: "app.example.org.", Type: "TXT", TTL: 300, Value: "v=spf1 include:example.org ~all"},
			wantType: dns.TypeTXT,
			check: func(t *testing.T, rr dns.RR) {
				txt := rr.(*dns.TXT)
				if len(txt.Txt) == 0 || txt.Txt[0] != "v=spf1 include:example.org ~all" {
					t.Errorf("TXT.Txt = %v, want [v=spf1 include:example.org ~all]", txt.Txt)
				}
			},
		},
		{
			name:     "MX record",
			record:   Record{Name: "example.org.", Type: "MX", TTL: 3600, Value: "mx1.example.org.", Priority: 10},
			wantType: dns.TypeMX,
			check: func(t *testing.T, rr dns.RR) {
				mx := rr.(*dns.MX)
				if mx.Preference != 10 {
					t.Errorf("MX.Preference = %d, want 10", mx.Preference)
				}
				if mx.Mx != "mx1.example.org." {
					t.Errorf("MX.Mx = %s, want mx1.example.org.", mx.Mx)
				}
			},
		},
		{
			name:     "SRV record",
			record:   Record{Name: "_sip._tcp.example.org.", Type: "SRV", TTL: 3600, Value: "sip.example.org.", Priority: 10, Weight: 60, Port: 5060},
			wantType: dns.TypeSRV,
			check: func(t *testing.T, rr dns.RR) {
				srv := rr.(*dns.SRV)
				if srv.Priority != 10 {
					t.Errorf("SRV.Priority = %d, want 10", srv.Priority)
				}
				if srv.Weight != 60 {
					t.Errorf("SRV.Weight = %d, want 60", srv.Weight)
				}
				if srv.Port != 5060 {
					t.Errorf("SRV.Port = %d, want 5060", srv.Port)
				}
				if srv.Target != "sip.example.org." {
					t.Errorf("SRV.Target = %s, want sip.example.org.", srv.Target)
				}
			},
		},
		{
			name:     "NS record",
			record:   Record{Name: "example.org.", Type: "NS", TTL: 3600, Value: "ns1.example.org."},
			wantType: dns.TypeNS,
			check: func(t *testing.T, rr dns.RR) {
				ns := rr.(*dns.NS)
				if ns.Ns != "ns1.example.org." {
					t.Errorf("NS.Ns = %s, want ns1.example.org.", ns.Ns)
				}
			},
		},
		{
			name:     "PTR record",
			record:   Record{Name: "1.0.0.10.in-addr.arpa.", Type: "PTR", TTL: 3600, Value: "app.example.org."},
			wantType: dns.TypePTR,
			check: func(t *testing.T, rr dns.RR) {
				ptr := rr.(*dns.PTR)
				if ptr.Ptr != "app.example.org." {
					t.Errorf("PTR.Ptr = %s, want app.example.org.", ptr.Ptr)
				}
			},
		},
		{
			name:     "CAA record",
			record:   Record{Name: "example.org.", Type: "CAA", TTL: 3600, Value: "letsencrypt.org", Flag: 0, Tag: "issue"},
			wantType: dns.TypeCAA,
			check: func(t *testing.T, rr dns.RR) {
				caa := rr.(*dns.CAA)
				if caa.Flag != 0 {
					t.Errorf("CAA.Flag = %d, want 0", caa.Flag)
				}
				if caa.Tag != "issue" {
					t.Errorf("CAA.Tag = %s, want issue", caa.Tag)
				}
				if caa.Value != "letsencrypt.org" {
					t.Errorf("CAA.Value = %s, want letsencrypt.org", caa.Value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rr, err := tt.record.ToRR()
			if err != nil {
				t.Fatalf("ToRR() unexpected error: %v", err)
			}
			if rr.Header().Rrtype != tt.wantType {
				t.Errorf("ToRR() type = %d, want %d", rr.Header().Rrtype, tt.wantType)
			}
			if rr.Header().Name != tt.record.Name {
				t.Errorf("ToRR() name = %s, want %s", rr.Header().Name, tt.record.Name)
			}
			if rr.Header().Ttl != tt.record.TTL {
				t.Errorf("ToRR() TTL = %d, want %d", rr.Header().Ttl, tt.record.TTL)
			}
			tt.check(t, rr)
		})
	}
}

func TestRecord_ToRR_UnsupportedType(t *testing.T) {
	t.Parallel()
	r := Record{Name: "app.example.org.", Type: "INVALID", TTL: 300, Value: "x"}
	_, err := r.ToRR()
	if err == nil {
		t.Fatal("ToRR() expected error for unsupported type")
	}
}

func TestRecord_TXT_LongValue(t *testing.T) {
	t.Parallel()
	// TXT records with values >255 chars should be split into chunks
	longValue := strings.Repeat("a", 300)
	r := Record{Name: "app.example.org.", Type: "TXT", TTL: 300, Value: longValue}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	rr, err := r.ToRR()
	if err != nil {
		t.Fatalf("ToRR() unexpected error: %v", err)
	}
	txt := rr.(*dns.TXT)
	if len(txt.Txt) < 2 {
		t.Errorf("expected TXT to be split into chunks, got %d chunk(s)", len(txt.Txt))
	}
	// Verify reconstructed value
	joined := strings.Join(txt.Txt, "")
	if joined != longValue {
		t.Errorf("reconstructed TXT value length = %d, want %d", len(joined), len(longValue))
	}
}
