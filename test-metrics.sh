#!/bin/bash
set -e

: "${TOK:?TOK environment variable must be set}"

URL="https://otlp-gateway-prod-gb-south-1.grafana.net/otlp/v1/metrics"
RULE_ACTIVE="${1:-0}"
LOAD="${2:-2.5}"
MEMORY="${3:-45.0}"
PHP_COUNT="${4:-8}"

# Convert rule_active (0 or 1) to seconds active in 1-min interval
if [ "$RULE_ACTIVE" = "1" ]; then
  RULE_SECONDS="60"
else
  RULE_SECONDS="0"
fi

TS=$(( $(date +%s) * 1000000000 ))

HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Basic $TOK" \
  -H "Content-Type: application/json" \
  -d "{\"resourceMetrics\":[{\"scopeMetrics\":[{\"metrics\":[{\"name\":\"bot_check_rule_active_seconds\",\"gauge\":{\"dataPoints\":[{\"asDouble\":\"$RULE_SECONDS\",\"timeUnixNano\":\"$TS\"}]}},{\"name\":\"load_average\",\"gauge\":{\"dataPoints\":[{\"asDouble\":\"$LOAD\",\"timeUnixNano\":\"$TS\"}]}},{\"name\":\"memory_percent\",\"gauge\":{\"dataPoints\":[{\"asDouble\":\"$MEMORY\",\"timeUnixNano\":\"$TS\"}]}},{\"name\":\"php_process_count\",\"gauge\":{\"dataPoints\":[{\"asDouble\":\"$PHP_COUNT\",\"timeUnixNano\":\"$TS\"}]}}]}]}]}" \
  "$URL")

echo "HTTP $HTTP — sent: rule_active_seconds=$RULE_SECONDS load=$LOAD memory=$MEMORY php_count=$PHP_COUNT"
