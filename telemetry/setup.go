package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/sdk/metric"
)

func nothing() {}

func Setup() (func(), error) {
	metricsCleanup, err := metrics()
	if err != nil {
		return nothing, err
	}
	return metricsCleanup, nil
}

// metrics attaches the prometheus collector to the default http server.
func metrics() (func(), error) {
	exporter, err := prometheus.New(prometheus.WithoutUnits())
	if err != nil {
		return nothing, err
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	http.Handle("/metrics", promhttp.Handler())
	global.SetMeterProvider(provider)

	// Need to shutdown the default http server.
	return nothing, nil
}
