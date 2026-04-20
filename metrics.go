package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// pushMetrics sends metrics to Grafana Cloud via OTLP JSON. It is a no-op if
// MetricsURL is not configured. values maps metric names to their float64 values.
func (a *app) pushMetrics(values map[string]float64) {
	if a.conf.MetricsURL == "" {
		return
	}

	slog.Debug("pushMetrics", "metrics", values)

	now := time.Now().UTC().UnixNano()
	metrics := make([]any, 0, len(values))

	for name, val := range values {
		metrics = append(metrics, map[string]any{
			"name": name,
			"gauge": map[string]any{
				"dataPoints": []any{
					map[string]any{
						"asDouble":      fmt.Sprintf("%.2f", val),
						"timeUnixNano": fmt.Sprintf("%d", now),
					},
				},
			},
		})
	}

	payload := map[string]any{
		"resourceMetrics": []any{
			map[string]any{
				"scopeMetrics": []any{
					map[string]any{
						"metrics": metrics,
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("pushMetrics: marshalling payload", "err", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, a.conf.MetricsURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("pushMetrics: creating request", "err", err)
		return
	}
	req.Header.Set("Authorization", "Basic "+a.conf.MetricsToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		slog.Warn("pushMetrics: sending metric", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		slog.Warn("pushMetrics: unexpected status", "status", resp.Status)
	} else {
		slog.Debug("pushMetrics: sent", "status", resp.Status)
	}
}
