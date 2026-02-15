// ABOUTME: DNS handler implementing plugin.Handler for the dynupdate plugin.
// ABOUTME: Serves records from the in-memory store with CNAME chasing and zone-aware fallthrough.

package dynupdate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

const (
	pluginName   = "dynupdate"
	maxCNAMEHops = 10
)

var log = clog.NewWithPlugin(pluginName)

// DynUpdate implements plugin.Handler for dynamic DNS record management.
type DynUpdate struct {
	Next  plugin.Handler
	Zones []string
	Store *Store
	Fall  fall.F
}

// Name returns the plugin name.
func (d *DynUpdate) Name() string { return pluginName }

// ServeDNS handles DNS queries by looking up records in the store.
func (d *DynUpdate) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.Name()
	qtype := state.QType()

	zone := plugin.Zones(d.Zones).Matches(qname)
	if zone == "" {
		return plugin.NextOrFailure(d.Name(), d.Next, ctx, w, r)
	}

	requestCount.WithLabelValues(zone).Inc()

	var rcode int
	var retErr error
	defer func() {
		responseCount.WithLabelValues(zone, dns.RcodeToString[rcode]).Inc()
	}()

	allRecords := d.Store.GetAll(qname)

	// No records for this name
	if len(allRecords) == 0 {
		if d.Fall.Through(qname) {
			rcode, retErr = plugin.NextOrFailure(d.Name(), d.Next, ctx, w, r)
			return rcode, retErr
		}
		rcode, retErr = d.writeNXDOMAIN(w, r, zone)
		return rcode, retErr
	}

	// Filter by query type
	typeRecords := filterByType(allRecords, qtype)
	if len(typeRecords) > 0 {
		rcode, retErr = d.writeAnswer(w, r, typeRecords)
		return rcode, retErr
	}

	// CNAME chasing for A/AAAA queries
	if qtype == dns.TypeA || qtype == dns.TypeAAAA {
		cnameRecords := filterByType(allRecords, dns.TypeCNAME)
		if len(cnameRecords) > 0 {
			chain := d.chaseCNAME(cnameRecords[0].Value, qtype, 1)
			// Build answer: CNAME + chain
			rr, err := cnameRecords[0].ToRR()
			if err == nil {
				answers := append([]dns.RR{rr}, chain...)
				rcode, retErr = d.writeAnswer(w, r, answers)
				return rcode, retErr
			}
		}
	}

	// Name exists but no matching type => NODATA
	rcode, retErr = d.writeNODATA(w, r, zone)
	return rcode, retErr
}

// chaseCNAME follows CNAME chains within the store, up to maxCNAMEHops depth.
func (d *DynUpdate) chaseCNAME(target string, qtype uint16, depth int) []dns.RR {
	if depth > maxCNAMEHops {
		return nil
	}

	allRecords := d.Store.GetAll(target)
	if len(allRecords) == 0 {
		return nil
	}

	// Check for the requested type at the target
	typeRecords := filterByType(allRecords, qtype)
	if len(typeRecords) > 0 {
		var rrs []dns.RR
		for _, rec := range typeRecords {
			rr, err := rec.ToRR()
			if err == nil {
				rrs = append(rrs, rr)
			}
		}
		return rrs
	}

	// Follow CNAME at the target
	cnameRecords := filterByType(allRecords, dns.TypeCNAME)
	if len(cnameRecords) > 0 {
		rr, err := cnameRecords[0].ToRR()
		if err != nil {
			return nil
		}
		rest := d.chaseCNAME(cnameRecords[0].Value, qtype, depth+1)
		return append([]dns.RR{rr}, rest...)
	}

	return nil
}

func filterByType(records []Record, qtype uint16) []Record {
	typeName := dns.TypeToString[qtype]
	var result []Record
	for _, r := range records {
		if strings.EqualFold(r.Type, typeName) {
			result = append(result, r)
		}
	}
	return result
}

func (d *DynUpdate) writeAnswer(w dns.ResponseWriter, r *dns.Msg, answers interface{}) (int, error) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	switch a := answers.(type) {
	case []Record:
		for _, rec := range a {
			rr, err := rec.ToRR()
			if err != nil {
				log.Errorf("converting record to RR: %v", err)
				continue
			}
			msg.Answer = append(msg.Answer, rr)
		}
	case []dns.RR:
		msg.Answer = append(msg.Answer, a...)
	}

	if err := w.WriteMsg(msg); err != nil {
		return dns.RcodeServerFailure, fmt.Errorf("writing response: %w", err)
	}
	return dns.RcodeSuccess, nil
}

func (d *DynUpdate) writeNXDOMAIN(w dns.ResponseWriter, r *dns.Msg, zone string) (int, error) {
	msg := new(dns.Msg)
	msg.SetRcode(r, dns.RcodeNameError)
	msg.Authoritative = true
	msg.Ns = []dns.RR{d.soa(zone)}

	if err := w.WriteMsg(msg); err != nil {
		return dns.RcodeServerFailure, fmt.Errorf("writing NXDOMAIN: %w", err)
	}
	return dns.RcodeNameError, nil
}

func (d *DynUpdate) writeNODATA(w dns.ResponseWriter, r *dns.Msg, zone string) (int, error) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true
	msg.Ns = []dns.RR{d.soa(zone)}

	if err := w.WriteMsg(msg); err != nil {
		return dns.RcodeServerFailure, fmt.Errorf("writing NODATA: %w", err)
	}
	return dns.RcodeSuccess, nil
}

func (d *DynUpdate) soa(zone string) dns.RR {
	return &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   zone,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Ns:      "ns1." + zone,
		Mbox:    "hostmaster." + zone,
		Serial:  uint32(time.Now().Unix()),
		Refresh: 7200,
		Retry:   1800,
		Expire:  86400,
		Minttl:  300,
	}
}
