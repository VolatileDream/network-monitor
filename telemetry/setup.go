package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/aggregation"
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
	exporter, err := prometheus.New(
		prometheus.WithoutUnits(),
		prometheus.WithAggregationSelector(overrideSelector))
	if err != nil {
		return nothing, err
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	http.Handle("/metrics", promhttp.Handler())
	global.SetMeterProvider(provider)

	// Need to shutdown the default http server.
	return nothing, nil
}

func overrideSelector(ik metric.InstrumentKind) aggregation.Aggregation {
	if ik != metric.InstrumentKindSyncHistogram {
		return metric.DefaultAggregationSelector(ik)
	}
	// For better resolution at the low end (where we hope latency stays), change
	// the histogram collections to squeeze an extra two buckets in.
	//
	// TODO: Ideally this would be configured on the latency metric itself.
	// It does not appear the otel library supports this (yet?).
	return aggregation.ExplicitBucketHistogram{
		// Constrasted with the default: {0, 5, 10, 25, 50, 75, 100, 250, 500, 1000}
		Boundaries: []float64{0, 2, 4, 8, 15, 25, 50, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
		NoMinMax:   false,
	}
}

var _ metric.AggregationSelector = overrideSelector
