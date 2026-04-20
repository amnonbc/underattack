package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// pushMetric sends the current bot check rule state to Grafana Cloud via OTLP
// JSON. It is a no-op if MetricsURL is not configured.
func (a *app) pushMetric(enabled bool) {
	if a.conf.MetricsURL == "" {
		return
	}

	value := 0
	if enabled {
		value = 1
	}

	payload := map[string]any{
		"resourceMetrics": []any{
			map[string]any{
				"scopeMetrics": []any{
					map[string]any{
						"metrics": []any{
							map[string]any{
								"name": "bot_check_rule_enabled",
								"gauge": map[string]any{
									"dataPoints": []any{
										map[string]any{
											"asInt":         fmt.Sprintf("%d", value),
											"timeUnixNano": fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("pushMetric: marshalling payload", "err", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, a.conf.MetricsURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("pushMetric: creating request", "err", err)
		return
	}
	req.Header.Set("Authorization", "Basic "+a.conf.MetricsToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		slog.Warn("pushMetric: sending metric", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		slog.Warn("pushMetric: unexpected status", "status", resp.Status)
	}
}
