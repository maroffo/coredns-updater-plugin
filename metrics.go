// ABOUTME: Prometheus metrics following the CoreDNS plugin convention.
// ABOUTME: Tracks DNS requests, response rcodes, API requests, and store record counts.

package dynupdate

import (
	"github.com/coredns/coredns/plugin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var requestCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: plugin.Namespace,
	Subsystem: "dynupdate",
	Name:      "request_count_total",
	Help:      "Counter of DNS requests handled.",
}, []string{"server"})

var responseCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: plugin.Namespace,
	Subsystem: "dynupdate",
	Name:      "response_rcode_count_total",
	Help:      "Counter of DNS responses by rcode.",
}, []string{"server", "rcode"})

var apiRequestCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: plugin.Namespace,
	Subsystem: "dynupdate",
	Name:      "api_request_count_total",
	Help:      "Counter of REST API requests.",
}, []string{"method", "status"})

var storeRecordGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: plugin.Namespace,
	Subsystem: "dynupdate",
	Name:      "store_records",
	Help:      "Current number of records in the store.",
}, []string{"type"})
