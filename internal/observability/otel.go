package observability

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// InitOTEL настраивает экспорт трейсов и метрик.
// Возвращает функцию shutdown и http.Handler для эндпоинта /metrics.
func InitOTEL(serviceName, otlpEndpoint string) (func(context.Context) error, http.Handler, error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 1. Trace Exporter (отправляем в Jaeger по gRPC/OTLP)
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(), // localhost без TLS
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// 2. Trace Provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	// Настраиваем пропагацию контекста (W3C TraceContext)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	// 3. Metric Exporter (Prometheus)
	promExporter, err := prometheus.New()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// 4. Meter Provider
	mp := metric.NewMeterProvider(metric.WithReader(promExporter))
	otel.SetMeterProvider(mp)

	log.Println("✅ OpenTelemetry initialized (Traces -> Jaeger, Metrics -> Prometheus)")

	shutdown := func(ctx context.Context) error {
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
		if err := mp.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down meter provider: %v", err)
		}
		return nil
	}

	return shutdown, promhttp.Handler(), nil
}
