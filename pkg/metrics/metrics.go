package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var (
	meter             metric.Meter
	queriesTotal      metric.Int64Counter
	queriesDuration   metric.Float64Histogram
	connectionsActive metric.Int64ObservableGauge
	cacheHitsTotal    metric.Int64Counter
	errorsTotal       metric.Int64Counter
	once              sync.Once
	exporter          *prometheus.Exporter
)

const (
	namespace = "udbproxy"
	subsystem = "proxy"
)

func Init(ctx context.Context) error {
	var err error
	once.Do(func() {
		exporter, err = prometheus.New()
		if err != nil {
			return
		}

		provider := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(exporter),
		)

		otel.SetMeterProvider(provider)
		meter = provider.Meter(namespace)

		queriesTotal, err = meter.Int64Counter(
			fmt.Sprintf("%s_%s_queries_total", namespace, subsystem),
			metric.WithDescription("Total number of queries processed"),
		)
		if err != nil {
			return
		}

		queriesDuration, err = meter.Float64Histogram(
			fmt.Sprintf("%s_%s_query_duration_seconds", namespace, subsystem),
			metric.WithDescription("Query duration in seconds"),
			metric.WithUnit("s"),
		)
		if err != nil {
			return
		}

		connectionsActive, err = meter.Int64ObservableGauge(
			fmt.Sprintf("%s_%s_connections_active", namespace, subsystem),
			metric.WithDescription("Active connections"),
		)
		if err != nil {
			return
		}

		cacheHitsTotal, err = meter.Int64Counter(
			fmt.Sprintf("%s_%s_cache_hits_total", namespace, subsystem),
			metric.WithDescription("Total number of cache hits"),
		)
		if err != nil {
			return
		}

		errorsTotal, err = meter.Int64Counter(
			fmt.Sprintf("%s_%s_errors_total", namespace, subsystem),
			metric.WithDescription("Total number of errors"),
		)
		if err != nil {
			return
		}
	})

	return err
}

func RecordQuery(ctx context.Context, dbType, operation, status string, duration time.Duration) {
	attrs := []attribute.KeyValue{
		attribute.String("db_type", dbType),
		attribute.String("operation", operation),
		attribute.String("status", status),
	}

	queriesTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	queriesDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

func RecordCacheHit(ctx context.Context, hit bool) {
	attrs := []attribute.KeyValue{
		attribute.Bool("hit", hit),
	}
	cacheHitsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func RecordError(ctx context.Context, errorType string) {
	attrs := []attribute.KeyValue{
		attribute.String("type", errorType),
	}
	errorsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func SetActiveConnections(ctx context.Context, count int64) {
}

func GetHandler() http.Handler {
	if exporter == nil {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	})
}
