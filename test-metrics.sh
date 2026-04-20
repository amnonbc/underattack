#!/bin/bash
set -e

: "${TOK:?TOK environment variable must be set}"

URL="https://otlp-gateway-prod-gb-south-1.grafana.net/otlp/v1/metrics"
VALUE="${1:-1}"

TS=$(( $(date +%s) * 1000000000 ))

HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Basic $TOK" \
  -H "Content-Type: application/json" \
  -d "{\"resourceMetrics\":[{\"scopeMetrics\":[{\"metrics\":[{\"name\":\"bot_check_rule_enabled\",\"gauge\":{\"dataPoints\":[{\"asInt\":\"$VALUE\",\"timeUnixNano\":\"$TS\"}]}}]}]}]}" \
  "$URL")

echo "HTTP $HTTP — sent bot_check_rule_enabled=$VALUE"
