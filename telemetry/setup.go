package telemetry

import (
	"net/http"

	honeycomb "github.com/honeycombio/honeycomb-opentelemetry-go"
	"github.com/honeycombio/opentelemetry-go-contrib/launcher"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/sdk/metric"
)

func nothing() {}

func Setup() (func(), error) {
	tracingCleanup, err := tracing()
	if err != nil {
		return nothing, err
	}
	metricsCleanup, err := metrics()
	if err != nil {
		return nothing, err
	}

	return func() {
		metricsCleanup()
		tracingCleanup()
	}, nil
}

// tracing consumes env variables to setup the tracer, most importantly:
// * OTEL_SERVICE_NAME
// * HONEYCOMB_API_KEY
func tracing() (func(), error) {
	bsp := honeycomb.NewBaggageSpanProcessor()

	// use honeycomb distro to setup OpenTelemetry SDK
	otelShutdown, err := launcher.ConfigureOpenTelemetry(
		launcher.WithSpanProcessor(bsp),
		// Prometheus is configured independantly.
		launcher.WithMetricsEnabled(false),
	)

	if err != nil {
		return nothing, err
	}
	return otelShutdown, nil
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
