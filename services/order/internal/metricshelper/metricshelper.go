package metricshelper

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HttpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed",
		},
		[]string{"path", "method", "status"},
	)

	HttpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Latency of HTTP requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path", "method", "status"},
	)

	SQSPublishTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqs_publish_total",
			Help: "Total number of SQS messages published",
		},
		[]string{"queue", "status"},
	)

	SQSPublishDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sqs_publish_duration_seconds",
			Help:    "Latency of SQS message publication in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"queue", "status"},
	)
)

// ResponseWriterWrapper to capture HTTP status codes
type ResponseWriterWrapper struct {
	http.ResponseWriter
	StatusCode int
}

func (w *ResponseWriterWrapper) WriteHeader(code int) {
	w.StatusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapper := &ResponseWriterWrapper{ResponseWriter: w, StatusCode: http.StatusOK}
		next.ServeHTTP(wrapper, r)

		duration := time.Since(start).Seconds()
		statusStr := strconv.Itoa(wrapper.StatusCode)

		HttpRequestsTotal.WithLabelValues(r.URL.Path, r.Method, statusStr).Inc()
		HttpRequestDuration.WithLabelValues(r.URL.Path, r.Method, statusStr).Observe(duration)
	})
}
