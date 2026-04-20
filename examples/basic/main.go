package main

import (
	"fmt"
	"os"

	"github.com/luminalog/sdk-go/luminalog"
)

func main() {
	logger, err := luminalog.New(luminalog.Config{
		APIKey:      env("LUMINALOG_API_KEY", "demo-api-key"),
		Environment: env("ENVIRONMENT", "development"),
		ProjectID:   "go-basic-example",
		Debug:       true,
	})
	if err != nil {
		panic(err)
	}
	defer logger.Shutdown()

	traceID := luminalog.GenerateTraceID()
	spanID := luminalog.GenerateSpanID()

	logger.Info("Go example started", map[string]interface{}{
		"runtime":  "go",
		"trace_id": traceID,
		"span_id":  spanID,
	})

	child := logger.Child(map[string]interface{}{
		"service": "billing-worker",
		"queue":   "invoices",
	})

	child.Warn("Worker latency elevated", map[string]interface{}{
		"trace_id":    traceID,
		"span_id":     spanID,
		"latency_ms":  990,
		"customer_id": "cus_123",
	})

	child.CaptureError(fmt.Errorf("invoice pdf generation failed"), map[string]interface{}{
		"trace_id":   traceID,
		"span_id":    spanID,
		"invoice_id": "inv_123",
	})

	logger.Flush()
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
