package main

import (
	"github.com/cloudflare/cloudflare-go"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_loadAvg(t *testing.T) {
	got, err := loadAvg("1.01 0.97 0.94 1/159 2795695")
	require.NoError(t, err)
	assert.Equal(t, []float64{1.01, 0.97, 0.94}, got)
}

func Test_loadAvgNil(t *testing.T) {
	_, err := loadAvg("")
	require.Error(t, err)
}

func Test_loadAvgNan(t *testing.T) {
	_, err := loadAvg("abc")
	require.Error(t, err)
}

const settingsResponse = `
{
  "result": [
    {
      "id": "0rtt",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "advanced_ddos",
      "value": "on",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "always_online",
      "value": "on",
      "modified_on": "2022-04-05T16:09:46.567846Z",
      "editable": true
    },
    {
      "id": "always_use_https",
      "value": "on",
      "modified_on": "2020-12-10T10:22:31.992316Z",
      "editable": true
    },
    {
      "id": "automatic_https_rewrites",
      "value": "on",
      "modified_on": "2022-02-20T08:32:57.145046Z",
      "editable": true
    },
    {
      "id": "brotli",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "browser_cache_ttl",
      "value": 14400,
      "modified_on": "2022-02-20T08:32:47.513143Z",
      "editable": true
    },
    {
      "id": "browser_check",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "cache_level",
      "value": "aggressive",
      "modified_on": "2022-02-20T08:32:45.589137Z",
      "editable": true
    },
    {
      "id": "challenge_ttl",
      "value": 86400,
      "modified_on": "2022-05-31T12:25:39.569035Z",
      "editable": true
    },
    {
      "id": "ciphers",
      "value": [],
      "modified_on": null,
      "editable": true
    },
    {
      "id": "cname_flattening",
      "value": "flatten_at_root",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "development_mode",
      "value": "off",
      "modified_on": "2022-06-07T16:57:16.418731Z",
      "time_remaining": 0,
      "editable": true
    },
    {
      "id": "early_hints",
      "value": "off",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "edge_cache_ttl",
      "value": 7200,
      "modified_on": null,
      "editable": true
    },
    {
      "id": "email_obfuscation",
      "value": "on",
      "modified_on": "2022-02-20T08:32:53.395946Z",
      "editable": true
    },
    {
      "id": "filter_logs_to_cloudflare",
      "value": "off",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "hotlink_protection",
      "modified_on": "2022-02-20T08:32:55.277008Z",
      "value": "off",
      "editable": true
    },
    {
      "id": "http2",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "http3",
      "value": "on",
      "modified_on": "2020-11-14T17:14:02.054863Z",
      "editable": true
    },
    {
      "id": "ip_geolocation",
      "value": "on",
      "modified_on": "2022-02-20T08:32:52.396066Z",
      "editable": true
    },
    {
      "id": "ipv6",
      "value": "on",
      "modified_on": "2020-11-12T17:22:08.917043Z",
      "editable": true
    },
    {
      "id": "log_to_cloudflare",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "long_lived_grpc",
      "value": "off",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "max_upload",
      "value": 100,
      "modified_on": null,
      "editable": true
    },
    {
      "id": "min_tls_version",
      "value": "1.0",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "minify",
      "value": {
        "js": "on",
        "css": "on",
        "html": "on"
      },
      "modified_on": "2022-03-06T19:18:58.484380Z",
      "editable": true
    },
    {
      "id": "mirage",
      "value": "on",
      "modified_on": "2020-12-10T13:35:14.123929Z",
      "editable": true
    },
    {
      "id": "mobile_redirect",
      "value": {
        "status": "off",
        "mobile_subdomain": "m",
        "strip_uri": false
      },
      "modified_on": "2022-02-28T23:01:41.527396Z",
      "editable": true
    },
    {
      "id": "opportunistic_encryption",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "opportunistic_onion",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "orange_to_orange",
      "value": "off",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "origin_error_page_pass_thru",
      "value": "off",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "polish",
      "value": "off",
      "modified_on": "2022-03-06T19:25:11.226341Z",
      "editable": true
    },
    {
      "id": "prefetch_preload",
      "value": "off",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "privacy_pass",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "proxy_read_timeout",
      "value": "100",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "pseudo_ipv4",
      "value": "off",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "response_buffering",
      "value": "off",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "rocket_loader",
      "value": "off",
      "modified_on": "2022-02-20T10:58:55.841179Z",
      "editable": true
    },
    {
      "id": "security_header",
      "modified_on": null,
      "value": {
        "strict_transport_security": {
          "enabled": false,
          "max_age": 0,
          "include_subdomains": false,
          "preload": false,
          "nosniff": false
        }
      },
      "editable": true
    },
    {
      "id": "security_level",
      "value": "medium",
      "modified_on": "2022-06-09T18:02:22.735012Z",
      "editable": true
    },
    {
      "id": "server_side_exclude",
      "value": "on",
      "modified_on": "2022-02-20T08:32:54.326689Z",
      "editable": true
    },
    {
      "id": "sort_query_string_for_cache",
      "value": "off",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "ssl",
      "value": "full",
      "modified_on": "2022-01-12T18:23:29.016049Z",
      "certificate_status": "active",
      "validation_errors": [],
      "editable": true
    },
    {
      "id": "tls_1_2_only",
      "value": "off",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "tls_1_3",
      "value": "zrt",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "tls_client_auth",
      "value": "off",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "true_client_ip_header",
      "value": "off",
      "modified_on": null,
      "editable": false
    },
    {
      "id": "visitor_ip",
      "value": "on",
      "modified_on": null,
      "editable": true
    },
    {
      "id": "waf",
      "value": "on",
      "modified_on": "2020-12-10T11:12:45.955358Z",
      "editable": true
    },
    {
      "id": "webp",
      "value": "on",
      "modified_on": "2022-03-06T19:17:51.539358Z",
      "editable": true
    },
    {
      "id": "websockets",
      "value": "on",
      "modified_on": "2022-02-20T08:32:51.407105Z",
      "editable": true
    }
  ],
  "success": true,
  "errors": [],
  "messages": []
}
`

func Test_app_currentLevel(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, settingsResponse)
	}))
	defer s.Close()
	a := app{}
	var err error
	a.api, err = cloudflare.New("key", "email")
	require.NoError(t, err)
	a.api.BaseURL = s.URL
	got, err := a.currentLevel()
	require.NoError(t, err)
	assert.Equal(t, "medium", got)
}
