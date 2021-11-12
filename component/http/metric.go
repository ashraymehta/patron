package http

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// MetricsPath of the service.
	MetricsPath = "/metrics"
)

func metricRoute() *RouteBuilder {
	return NewRawRouteBuilder(MetricsPath, promhttp.Handler().ServeHTTP).MethodGet()
}
