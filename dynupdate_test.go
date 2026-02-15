// ABOUTME: Tests for the DNS handler: ServeDNS, zone matching, CNAME chasing, SOA, fallthrough.
// ABOUTME: Uses the CoreDNS test.ResponseWriter for capturing DNS responses.

package dynupdate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
)

func newTestHandler(t *testing.T, records []Record) *DynUpdate {
	t.Helper()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	s, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	t.Cleanup(func() { s.Stop() })

	for _, r := range records {
		if err := s.Upsert(r); err != nil {
			t.Fatalf("Upsert(%v) error: %v", r, err)
		}
	}

	return &DynUpdate{
		Zones: []string{"example.org."},
		Store: s,
	}
}

func TestServeDNS_A_Record(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	})

	req := new(dns.Msg)
	req.SetQuestion("app.example.org.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	if len(rec.Msg.Answer) != 1 {
		t.Fatalf("got %d answers, want 1", len(rec.Msg.Answer))
	}
	a, ok := rec.Msg.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer is %T, want *dns.A", rec.Msg.Answer[0])
	}
	if a.A.String() != "10.0.0.1" {
		t.Errorf("A = %s, want 10.0.0.1", a.A)
	}
	if !rec.Msg.Authoritative {
		t.Error("Authoritative = false, want true")
	}
}

func TestServeDNS_AAAA_Record(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "app.example.org.", Type: "AAAA", TTL: 300, Value: "2001:db8::1"},
	})

	req := new(dns.Msg)
	req.SetQuestion("app.example.org.", dns.TypeAAAA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	if len(rec.Msg.Answer) != 1 {
		t.Fatalf("got %d answers, want 1", len(rec.Msg.Answer))
	}
}

func TestServeDNS_MX_Record(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "example.org.", Type: "MX", TTL: 3600, Value: "mx1.example.org.", Priority: 10},
	})

	req := new(dns.Msg)
	req.SetQuestion("example.org.", dns.TypeMX)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	mx := rec.Msg.Answer[0].(*dns.MX)
	if mx.Preference != 10 {
		t.Errorf("MX.Preference = %d, want 10", mx.Preference)
	}
}

func TestServeDNS_NXDOMAIN(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, nil)

	req := new(dns.Msg)
	req.SetQuestion("nonexistent.example.org.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeNameError {
		t.Errorf("rcode = %d, want %d (NXDOMAIN)", code, dns.RcodeNameError)
	}
	if len(rec.Msg.Ns) == 0 {
		t.Error("expected SOA in authority section for NXDOMAIN")
	}
}

func TestServeDNS_NODATA(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	})

	// Query for AAAA when only A exists
	req := new(dns.Msg)
	req.SetQuestion("app.example.org.", dns.TypeAAAA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d (NODATA)", code, dns.RcodeSuccess)
	}
	if len(rec.Msg.Answer) != 0 {
		t.Errorf("expected no answers for NODATA, got %d", len(rec.Msg.Answer))
	}
	if len(rec.Msg.Ns) == 0 {
		t.Error("expected SOA in authority section for NODATA")
	}
}

func TestServeDNS_CNAME_Chasing_SingleHop(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "alias.example.org.", Type: "CNAME", TTL: 300, Value: "app.example.org."},
		{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	})

	req := new(dns.Msg)
	req.SetQuestion("alias.example.org.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	if len(rec.Msg.Answer) != 2 {
		t.Fatalf("got %d answers, want 2 (CNAME + A)", len(rec.Msg.Answer))
	}
	if _, ok := rec.Msg.Answer[0].(*dns.CNAME); !ok {
		t.Errorf("first answer is %T, want *dns.CNAME", rec.Msg.Answer[0])
	}
	if _, ok := rec.Msg.Answer[1].(*dns.A); !ok {
		t.Errorf("second answer is %T, want *dns.A", rec.Msg.Answer[1])
	}
}

func TestServeDNS_CNAME_Chasing_MultiHop(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "a.example.org.", Type: "CNAME", TTL: 300, Value: "b.example.org."},
		{Name: "b.example.org.", Type: "CNAME", TTL: 300, Value: "c.example.org."},
		{Name: "c.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
	})

	req := new(dns.Msg)
	req.SetQuestion("a.example.org.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	// CNAME a→b, CNAME b→c, A c
	if len(rec.Msg.Answer) != 3 {
		t.Fatalf("got %d answers, want 3 (2 CNAMEs + A)", len(rec.Msg.Answer))
	}
}

func TestServeDNS_CNAME_LoopProtection(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "loop1.example.org.", Type: "CNAME", TTL: 300, Value: "loop2.example.org."},
		{Name: "loop2.example.org.", Type: "CNAME", TTL: 300, Value: "loop1.example.org."},
	})

	req := new(dns.Msg)
	req.SetQuestion("loop1.example.org.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	// Should return the CNAME chain without infinite loop
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	// Should have answers (the CNAME chain up to max depth)
	if len(rec.Msg.Answer) == 0 {
		t.Error("expected at least one CNAME answer for loop")
	}
}

func TestServeDNS_OutOfZone_PassToNext(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, nil)
	d.Next = test.NextHandler(dns.RcodeSuccess, nil)

	req := new(dns.Msg)
	req.SetQuestion("app.other.com.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("out-of-zone query: rcode = %d, want pass-through", code)
	}
}

func TestServeDNS_Fallthrough(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, nil)
	d.Fall = fall.F{}
	d.Fall.SetZonesFromArgs([]string{})
	d.Next = test.NextHandler(dns.RcodeSuccess, nil)

	req := new(dns.Msg)
	req.SetQuestion("missing.example.org.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("fallthrough: rcode = %d, want pass-through", code)
	}
}

func TestServeDNS_MultipleARecords(t *testing.T) {
	t.Parallel()
	d := newTestHandler(t, []Record{
		{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"},
		{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"},
	})

	req := new(dns.Msg)
	req.SetQuestion("app.example.org.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	code, err := d.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatalf("ServeDNS() error: %v", err)
	}
	if code != dns.RcodeSuccess {
		t.Errorf("rcode = %d, want %d", code, dns.RcodeSuccess)
	}
	if len(rec.Msg.Answer) != 2 {
		t.Errorf("got %d answers, want 2", len(rec.Msg.Answer))
	}
}

func TestDynUpdate_Name(t *testing.T) {
	t.Parallel()
	d := &DynUpdate{}
	if d.Name() != "dynupdate" {
		t.Errorf("Name() = %q, want %q", d.Name(), "dynupdate")
	}
}
