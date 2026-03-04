package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestApp() *app {
	return &app{maxLoad: 4.5, minLoad: 1.0, maxProcs: 20}
}

// rulesetServer creates a fake Cloudflare API server. It tracks the enabled
// state of a single rule and the number of PATCH calls received.
// Returns the server, a pointer to the rule's enabled state, and a pointer to
// the PATCH call count.
func rulesetServer(t *testing.T, zoneID, rulesetID, ruleID string, initialEnabled bool) (*httptest.Server, *bool, *int) {
	t.Helper()
	enabled := new(bool)
	*enabled = initialEnabled
	patchCount := new(int)

	mux := http.NewServeMux()

	mux.HandleFunc("/zones", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]string{{"id": zoneID, "name": "example.com"}},
		})
	})

	mux.HandleFunc(fmt.Sprintf("/zones/%s/rulesets/%s", zoneID, rulesetID),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"rules": []map[string]any{{"id": ruleID, "enabled": *enabled}},
				},
			})
		})

	mux.HandleFunc(fmt.Sprintf("/zones/%s/rulesets/%s/rules/%s", zoneID, rulesetID, ruleID),
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPatch {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			*patchCount++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			*enabled = body["enabled"].(bool)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"rules": []map[string]any{{"id": ruleID, "enabled": *enabled}},
				},
			})
		})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, enabled, patchCount
}

// appForServer wires an app to a test server: sets baseURL to ts.URL so all
// CF URL construction resolves there, and sets client to ts.Client() so
// requests are routed through the test server's transport.
func appForServer(ts *httptest.Server, zoneID, rulesetID, ruleID string) *app {
	return &app{
		maxLoad:  4.5,
		minLoad:  1.0,
		maxProcs: 9999,
		client:   ts.Client(),
		baseURL:  ts.URL,
		zoneId:   zoneID,
		conf: Config{
			ApiKey:    "test-key",
			RulesetID: rulesetID,
			RuleID:    ruleID,
		},
	}
}

func writeTempLoadFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "loadavg-*")
	if err != nil {
		t.Fatalf("creating temp load file: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func TestLoadConfig_Valid(t *testing.T) {
	cfg := Config{Domain: "example.com", ApiKey: "key123", DbName: "mydb", DbUser: "user", DbPassword: "pass"}
	data, _ := json.Marshal(cfg)
	f, _ := os.CreateTemp("", "ua-cfg-*.json")
	defer os.Remove(f.Name())
	f.Write(data)
	f.Close()

	a := newTestApp()
	if err := a.loadConfig(f.Name()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.conf.Domain != cfg.Domain {
		t.Errorf("Domain = %q, want %q", a.conf.Domain, cfg.Domain)
	}
	if a.conf.ApiKey != cfg.ApiKey {
		t.Errorf("ApiKey = %q, want %q", a.conf.ApiKey, cfg.ApiKey)
	}
	if a.conf.DbName != cfg.DbName {
		t.Errorf("DbName = %q, want %q", a.conf.DbName, cfg.DbName)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	if err := newTestApp().loadConfig("/nonexistent/path.json"); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	f, _ := os.CreateTemp("", "ua-bad-*.json")
	defer os.Remove(f.Name())
	f.WriteString("{not valid json")
	f.Close()
	if err := newTestApp().loadConfig(f.Name()); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// loadAvg
// ---------------------------------------------------------------------------

func TestLoadAvg_Normal(t *testing.T) {
	cases := []struct {
		input string
		want  [3]float64
	}{
		{"0.10 0.20 0.30 1/100 12345", [3]float64{0.10, 0.20, 0.30}},
		{"3.50 2.10 1.80 4/512 99999", [3]float64{3.50, 2.10, 1.80}},
		{"0.00 0.01 0.05 2/64 1", [3]float64{0.00, 0.01, 0.05}},
		{"12.34 5.67 3.21 8/200 55555", [3]float64{12.34, 5.67, 3.21}},
	}
	for _, tc := range cases {
		got, err := loadAvg(tc.input)
		if err != nil {
			t.Errorf("loadAvg(%q) error: %v", tc.input, err)
			continue
		}
		if len(got) != 3 {
			t.Errorf("loadAvg(%q) returned %d values, want 3", tc.input, len(got))
			continue
		}
		for i, w := range tc.want {
			if got[i] != w {
				t.Errorf("loadAvg(%q)[%d] = %v, want %v", tc.input, i, got[i], w)
			}
		}
	}
}

func TestLoadAvg_TooFewFields(t *testing.T) {
	if _, err := loadAvg("0.10 0.20"); err == nil {
		t.Error("expected error for too few fields, got nil")
	}
}

func TestLoadAvg_EmptyString(t *testing.T) {
	if _, err := loadAvg(""); err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

func TestLoadAvg_NonNumeric(t *testing.T) {
	if _, err := loadAvg("high load 0.50 3/100"); err == nil {
		t.Error("expected error for non-numeric field, got nil")
	}
}

// ---------------------------------------------------------------------------
// allBelow
// ---------------------------------------------------------------------------

func TestAllBelow_AllUnder(t *testing.T) {
	if !allBelow([]float64{0.1, 0.2, 0.3}, 1.0) {
		t.Error("expected true: all values below 1.0")
	}
}

func TestAllBelow_OneEqual(t *testing.T) {
	// allBelow uses >=, so a value equal to the threshold is not "below".
	if allBelow([]float64{0.5, 1.0, 0.3}, 1.0) {
		t.Error("expected false: 1.0 is not strictly below 1.0")
	}
}

func TestAllBelow_OneAbove(t *testing.T) {
	if allBelow([]float64{0.5, 1.5, 0.3}, 1.0) {
		t.Error("expected false: 1.5 >= 1.0")
	}
}

func TestAllBelow_EmptySlice(t *testing.T) {
	if !allBelow([]float64{}, 1.0) {
		t.Error("expected true for empty slice")
	}
}

// ---------------------------------------------------------------------------
// NewRequest
// ---------------------------------------------------------------------------

func TestNewRequest_SetsAuthHeader(t *testing.T) {
	a := newTestApp()
	a.conf.ApiKey = "my-secret-key"
	req, err := a.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer my-secret-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer my-secret-key")
	}
}

func TestNewRequest_SetsContentType(t *testing.T) {
	a := newTestApp()
	req, err := a.NewRequest("POST", "http://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestNewRequest_Methods(t *testing.T) {
	a := newTestApp()
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		req, err := a.NewRequest(method, "http://example.com", nil)
		if err != nil {
			t.Fatalf("NewRequest(%q) error: %v", method, err)
		}
		if req.Method != method {
			t.Errorf("Method = %q, want %q", req.Method, method)
		}
	}
}

func TestNewRequest_InvalidURL(t *testing.T) {
	if _, err := newTestApp().NewRequest("GET", "://bad-url", nil); err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// getRuleState
// ---------------------------------------------------------------------------

func TestGetRuleState_RuleEnabled(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z1", "rs1", "rule1"
	)
	ts, _, _ := rulesetServer(t, zoneID, rulesetID, ruleID, true)

	a := appForServer(ts, zoneID, rulesetID, ruleID)
	got, err := a.getRuleState()
	if err != nil {
		t.Fatalf("getRuleState error: %v", err)
	}
	if !got {
		t.Error("expected rule enabled=true")
	}
}

func TestGetRuleState_RuleDisabled(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z2", "rs1", "rule1"
	)
	ts, _, _ := rulesetServer(t, zoneID, rulesetID, ruleID, false)

	a := appForServer(ts, zoneID, rulesetID, ruleID)
	got, err := a.getRuleState()
	if err != nil {
		t.Fatalf("getRuleState error: %v", err)
	}
	if got {
		t.Error("expected rule enabled=false")
	}
}

func TestGetRuleState_RuleNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"rules": []map[string]any{{"id": "other-rule", "enabled": true}},
			},
		})
	}))
	t.Cleanup(ts.Close)

	a := appForServer(ts, "z3", "rs1", "missing-rule")
	if _, err := a.getRuleState(); err == nil {
		t.Error("expected error when rule not found, got nil")
	}
}

func TestGetRuleState_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	a := appForServer(ts, "z4", "rs1", "rule1")
	if _, err := a.getRuleState(); err == nil {
		t.Error("expected error on HTTP 500, got nil")
	}
}

// ---------------------------------------------------------------------------
// setRuleEnabled
// ---------------------------------------------------------------------------

func TestSetRuleEnabled_EnablesRule(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z5", "rs1", "rule1"
	)
	ts, enabled, patchCount := rulesetServer(t, zoneID, rulesetID, ruleID, false)

	a := appForServer(ts, zoneID, rulesetID, ruleID)
	if err := a.setRuleEnabled(true); err != nil {
		t.Fatalf("setRuleEnabled(true) error: %v", err)
	}
	if !*enabled {
		t.Error("expected rule enabled=true")
	}
	if *patchCount != 1 {
		t.Errorf("patchCount = %d, want 1", *patchCount)
	}
}

func TestSetRuleEnabled_DisablesRule(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z6", "rs1", "rule1"
	)
	ts, enabled, patchCount := rulesetServer(t, zoneID, rulesetID, ruleID, true)

	a := appForServer(ts, zoneID, rulesetID, ruleID)
	if err := a.setRuleEnabled(false); err != nil {
		t.Fatalf("setRuleEnabled(false) error: %v", err)
	}
	if *enabled {
		t.Error("expected rule enabled=false")
	}
	if *patchCount != 1 {
		t.Errorf("patchCount = %d, want 1", *patchCount)
	}
}

func TestSetRuleEnabled_IdempotentWhenAlreadyEnabled(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z7", "rs1", "rule1"
	)
	ts, _, patchCount := rulesetServer(t, zoneID, rulesetID, ruleID, true)

	a := appForServer(ts, zoneID, rulesetID, ruleID)
	if err := a.setRuleEnabled(true); err != nil {
		t.Fatalf("setRuleEnabled(true) error: %v", err)
	}
	if *patchCount != 0 {
		t.Errorf("expected 0 PATCHes when already enabled, got %d", *patchCount)
	}
}

func TestSetRuleEnabled_IdempotentWhenAlreadyDisabled(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z8", "rs1", "rule1"
	)
	ts, _, patchCount := rulesetServer(t, zoneID, rulesetID, ruleID, false)

	a := appForServer(ts, zoneID, rulesetID, ruleID)
	if err := a.setRuleEnabled(false); err != nil {
		t.Fatalf("setRuleEnabled(false) error: %v", err)
	}
	if *patchCount != 0 {
		t.Errorf("expected 0 PATCHes when already disabled, got %d", *patchCount)
	}
}

// ---------------------------------------------------------------------------
// doIt — integration tests with a real load file and fake CF server.
// checkDb will fail (no MySQL), which triggers setRuleEnabled(true) via the
// DB-failure path. For the low-load and mid-range tests we need checkDb to
// succeed; sql.Open + db.Close on an empty DSN succeeds without dialing,
// so we leave DbUser/DbPassword empty.
// ---------------------------------------------------------------------------

func newDoItApp(t *testing.T, ts *httptest.Server, loadContent, zoneID, rulesetID, ruleID string) *app {
	t.Helper()
	a := appForServer(ts, zoneID, rulesetID, ruleID)
	a.loadFile = writeTempLoadFile(t, loadContent)
	return a
}

func TestDoIt_HighLoadEnablesRule(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z9", "rs2", "rule2"
	)
	ts, enabled, _ := rulesetServer(t, zoneID, rulesetID, ruleID, false)

	a := newDoItApp(t, ts, "10.00 8.00 6.00 5/200 12345", zoneID, rulesetID, ruleID)
	a.doIt()

	if !*enabled {
		t.Error("expected rule enabled=true when load is above maxLoad")
	}
}

func TestDoIt_LowLoadDisablesRule(t *testing.T) {
	const (
		zoneID, rulesetID, ruleID = "z10", "rs2", "rule2"
	)
	ts, enabled, _ := rulesetServer(t, zoneID, rulesetID, ruleID, true)

	// Empty DB credentials: sql.Open + db.Close succeeds without a real server.
	a := newDoItApp(t, ts, "0.10 0.20 0.30 1/100 12345", zoneID, rulesetID, ruleID)
	a.doIt()

	if *enabled {
		t.Error("expected rule enabled=false when load is below minLoad")
	}
}

func TestDoIt_MidRangeLoadNoChange(t *testing.T) {
	// Load between minLoad (1.0) and maxLoad (4.5) — no PATCH expected.
	const (
		zoneID, rulesetID, ruleID = "z11", "rs2", "rule2"
	)
	ts, _, patchCount := rulesetServer(t, zoneID, rulesetID, ruleID, false)

	a := newDoItApp(t, ts, "2.00 1.50 1.20 3/100 12345", zoneID, rulesetID, ruleID)
	a.doIt()

	if *patchCount != 0 {
		t.Errorf("expected 0 PATCHes for mid-range load, got %d", *patchCount)
	}
}

func TestDoIt_ExactlyAtMaxLoadEnablesRule(t *testing.T) {
	// la[0] >= maxLoad (4.5) must trigger enable.
	const (
		zoneID, rulesetID, ruleID = "z12", "rs2", "rule2"
	)
	ts, enabled, _ := rulesetServer(t, zoneID, rulesetID, ruleID, false)

	a := newDoItApp(t, ts, "4.50 1.50 1.00 3/100 12345", zoneID, rulesetID, ruleID)
	a.doIt()

	if !*enabled {
		t.Error("expected rule enabled=true when load equals maxLoad")
	}
}
