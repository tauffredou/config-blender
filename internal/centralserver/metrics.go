package centralserver

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metrics holds the central service's Prometheus collectors. Each Server
// gets its own registry (rather than the global default) so multiple
// Servers can coexist in one process — as the test suite does — without
// "duplicate metrics collector registration" panics.
type metrics struct {
	registry        *prometheus.Registry
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()
	m := &metrics{
		registry: reg,
		requestsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "configblender_central_http_requests_total",
			Help: "Total HTTP requests handled by the central service, by method, path and status.",
		}, []string{"method", "path", "status"}),
		requestDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "configblender_central_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, by method and path.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),
	}
	return m
}

func (m *metrics) observe(method, path string, status int, duration time.Duration) {
	m.requestsTotal.WithLabelValues(method, path, strconv.Itoa(status)).Inc()
	m.requestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
