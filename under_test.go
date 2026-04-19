package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestApp() *app {
	return &app{maxLoad: 4.5, minLoad: 1.0, maxProcs: 20}
}

type testRule struct {
	ID          string
	Description string
	Expression  string
}

// rulesetServer creates a fake Cloudflare API server backed by an in-memory
// rule list. It handles GET (list rules), POST (create rule), and DELETE
// (delete rule). Returns the server and a pointer to the rule slice.
func rulesetServer(t *testing.T, zoneID, rulesetID string, initial []testRule) (*httptest.Server, *[]testRule) {
	t.Helper()
	var mu sync.Mutex
	rules := make([]testRule, len(initial))
	copy(rules, initial)
	nextID := 100

	mux := http.NewServeMux()

	mux.HandleFunc("/zones", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []map[string]string{{"id": zoneID, "name": "example.com"}},
		})
	})

	rulesetPath := fmt.Sprintf("/zones/%s/rulesets/%s", zoneID, rulesetID)
	mux.HandleFunc(rulesetPath, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		result := make([]map[string]any, len(rules))
		for i, rule := range rules {
			result[i] = map[string]any{"id": rule.ID, "description": rule.Description, "expression": rule.Expression}
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"rules": result}})
	})

	rulesPath := fmt.Sprintf("/zones/%s/rulesets/%s/rules", zoneID, rulesetID)
	mux.HandleFunc(rulesPath+"/", func(w http.ResponseWriter, r *http.Request) {
		// DELETE /zones/{z}/rulesets/{rs}/rules/{id}
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ruleID := strings.TrimPrefix(r.URL.Path, rulesPath+"/")
		mu.Lock()
		defer mu.Unlock()
		for i, rule := range rules {
			if rule.ID == ruleID {
				rules = append(rules[:i], rules[i+1:]...)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"rules": rules}})
				return
			}
		}
		http.Error(w, "rule not found", http.StatusNotFound)
	})

	mux.HandleFunc(rulesPath, func(w http.ResponseWriter, r *http.Request) {
		// POST /zones/{z}/rulesets/{rs}/rules
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		nextID++
		rule := testRule{
			ID:          fmt.Sprintf("rule-%d", nextID),
			Description: body["description"].(string),
			Expression:  body["expression"].(string),
		}
		rules = append(rules, rule)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"rules": []map[string]any{{"id": rule.ID, "description": rule.Description, "expression": rule.Expression}},
			},
		})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, &rules
}

func appForServer(ts *httptest.Server, zoneID, rulesetID string) *app {
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
	cfg := Config{Domain: "example.com", ApiKey: "key123", DbName: "mydb", DbUser: "user", DbPassword: "pass", RulesetID: "rs1"}
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
// findRule
// ---------------------------------------------------------------------------

func TestFindRule_Found(t *testing.T) {
	ts, _ := rulesetServer(t, "z1", "rs1", []testRule{{ID: "rule-1", Description: botCheckDescription, Expression: "expr"}})
	a := appForServer(ts, "z1", "rs1")
	info, err := a.findRule()
	if err != nil {
		t.Fatalf("findRule error: %v", err)
	}
	if info == nil || info.ID != "rule-1" {
		t.Errorf("findRule ID = %v, want rule-1", info)
	}
	if info.Expression != "expr" {
		t.Errorf("findRule Expression = %q, want %q", info.Expression, "expr")
	}
}

func TestFindRule_NotFound(t *testing.T) {
	ts, _ := rulesetServer(t, "z2", "rs1", []testRule{{ID: "rule-1", Description: "some other rule"}})
	a := appForServer(ts, "z2", "rs1")
	info, err := a.findRule()
	if err != nil {
		t.Fatalf("findRule error: %v", err)
	}
	if info != nil {
		t.Errorf("findRule = %v, want nil", info)
	}
}

func TestFindRule_EmptyRuleset(t *testing.T) {
	ts, _ := rulesetServer(t, "z3", "rs1", nil)
	a := appForServer(ts, "z3", "rs1")
	info, err := a.findRule()
	if err != nil {
		t.Fatalf("findRule error: %v", err)
	}
	if info != nil {
		t.Errorf("findRule = %v, want nil", info)
	}
}

// ---------------------------------------------------------------------------
// ensureBotCheck
// ---------------------------------------------------------------------------

func TestEnsureBotCheck_CreatesRuleWhenNoneExists(t *testing.T) {
	ts, rules := rulesetServer(t, "z4", "rs1", nil)
	a := appForServer(ts, "z4", "rs1")
	if err := a.ensureBotCheck(true, "test"); err != nil {
		t.Fatalf("ensureBotCheck(true) error: %v", err)
	}
	if len(*rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(*rules))
	}
	if (*rules)[0].Description != botCheckDescription {
		t.Errorf("rule description = %q, want %q", (*rules)[0].Description, botCheckDescription)
	}
}

func TestEnsureBotCheck_ReplacesExistingRuleToRefreshExpression(t *testing.T) {
	stale := testRule{ID: "old-rule", Description: botCheckDescription, Expression: "old expression"}
	ts, rules := rulesetServer(t, "z5", "rs1", []testRule{stale})
	a := appForServer(ts, "z5", "rs1")
	if err := a.ensureBotCheck(true, "test"); err != nil {
		t.Fatalf("ensureBotCheck(true) error: %v", err)
	}
	if len(*rules) != 1 {
		t.Fatalf("expected 1 rule after replace, got %d", len(*rules))
	}
	if (*rules)[0].ID == "old-rule" {
		t.Error("old rule should have been replaced")
	}
	if (*rules)[0].Expression == "old expression" {
		t.Error("expression should have been updated")
	}
}

func TestEnsureBotCheck_DeletesRuleWhenInactive(t *testing.T) {
	existing := testRule{ID: "rule-1", Description: botCheckDescription}
	ts, rules := rulesetServer(t, "z6", "rs1", []testRule{existing})
	a := appForServer(ts, "z6", "rs1")
	if err := a.ensureBotCheck(false, ""); err != nil {
		t.Fatalf("ensureBotCheck(false) error: %v", err)
	}
	if len(*rules) != 0 {
		t.Errorf("expected 0 rules after deactivation, got %d", len(*rules))
	}
}

func TestEnsureBotCheck_NoChurnWhenTodayAlreadyInExpression(t *testing.T) {
	today := time.Now().Format("02-01-2006")
	current := testRule{ID: "rule-1", Description: botCheckDescription, Expression: "/" + today + "/"}
	ts, rules := rulesetServer(t, "z7a", "rs1", []testRule{current})
	a := appForServer(ts, "z7a", "rs1")
	if err := a.ensureBotCheck(true, "test"); err != nil {
		t.Fatalf("ensureBotCheck(true) error: %v", err)
	}
	if len(*rules) != 1 || (*rules)[0].ID != "rule-1" {
		t.Error("rule should not have been replaced when today's date is already in expression")
	}
}

func TestEnsureBotCheck_NoopWhenInactiveAndNoRule(t *testing.T) {
	ts, rules := rulesetServer(t, "z7", "rs1", nil)
	a := appForServer(ts, "z7", "rs1")
	if err := a.ensureBotCheck(false, ""); err != nil {
		t.Fatalf("ensureBotCheck(false) error: %v", err)
	}
	if len(*rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(*rules))
	}
}

// ---------------------------------------------------------------------------
// buildExpression
// ---------------------------------------------------------------------------

func TestBuildExpression_ContainsBaseConditions(t *testing.T) {
	expr := buildExpression()
	for _, want := range []string{
		`http.request.uri.path contains "/articles/"`,
		`http.request.method eq "GET"`,
		`not cf.client.bot`,
		`not http.cookie contains "wordpress_logged_in"`,
	} {
		if !strings.Contains(expr, want) {
			t.Errorf("expression missing %q", want)
		}
	}
}

func TestBuildExpression_ExemptsRecentDates(t *testing.T) {
	expr := buildExpression()
	// tomorrow through 7 days ago (9 dates total)
	for i := range 9 {
		d := time.Now().AddDate(0, 0, 1-i).Format("02-01-2006")
		want := fmt.Sprintf(`"/%s/"`, d)
		if !strings.Contains(expr, want) {
			t.Errorf("expression missing exemption for date %s", d)
		}
	}
}

func TestBuildExpression_ExemptsTomorrow(t *testing.T) {
	expr := buildExpression()
	tomorrow := time.Now().AddDate(0, 0, 1).Format("02-01-2006")
	if !strings.Contains(expr, tomorrow) {
		t.Errorf("expression should exempt tomorrow (%s) for timezone offset", tomorrow)
	}
}

func TestBuildExpression_DoesNotExemptOldDate(t *testing.T) {
	expr := buildExpression()
	old := time.Now().AddDate(0, 0, -8).Format("02-01-2006")
	if strings.Contains(expr, old) {
		t.Errorf("expression should not exempt date 8 days ago (%s)", old)
	}
}

func TestBuildExpression_HasNineDateExemptions(t *testing.T) {
	expr := buildExpression()
	if !strings.Contains(expr, "not (") {
		t.Error("expression missing 'not (' for date exemptions")
	}
	count := 0
	for i := range 9 {
		d := time.Now().AddDate(0, 0, 1-i).Format("02-01-2006")
		if strings.Contains(expr, d) {
			count++
		}
	}
	if count != 9 {
		t.Errorf("expected 9 date exemptions, found %d", count)
	}
}

// ---------------------------------------------------------------------------
// doIt — integration tests with a real load file and fake CF server.
// ---------------------------------------------------------------------------

func newDoItApp(t *testing.T, ts *httptest.Server, loadContent, zoneID, rulesetID string) *app {
	t.Helper()
	a := appForServer(ts, zoneID, rulesetID)
	a.loadFile = writeTempLoadFile(t, loadContent)
	return a
}

func TestDoIt_HighLoadEnablesRule(t *testing.T) {
	ts, rules := rulesetServer(t, "z9", "rs2", nil)
	a := newDoItApp(t, ts, "10.00 8.00 6.00 5/200 12345", "z9", "rs2")
	a.doIt()
	if len(*rules) != 1 {
		t.Errorf("expected 1 rule when load is above maxLoad, got %d", len(*rules))
	}
}

func TestDoIt_LowLoadDeletesRule(t *testing.T) {
	existing := testRule{ID: "rule-1", Description: botCheckDescription}
	ts, rules := rulesetServer(t, "z10", "rs2", []testRule{existing})
	a := newDoItApp(t, ts, "0.10 0.20 0.30 1/100 12345", "z10", "rs2")
	a.doIt()
	if len(*rules) != 0 {
		t.Errorf("expected 0 rules when load is below minLoad, got %d", len(*rules))
	}
}

func TestDoIt_MidRangeLoadNoChange(t *testing.T) {
	ts, rules := rulesetServer(t, "z11", "rs2", nil)
	a := newDoItApp(t, ts, "2.00 1.50 1.20 3/100 12345", "z11", "rs2")
	a.doIt()
	if len(*rules) != 0 {
		t.Errorf("expected 0 rule changes for mid-range load, got %d rules", len(*rules))
	}
}

func TestDoIt_ExactlyAtMaxLoadEnablesRule(t *testing.T) {
	ts, rules := rulesetServer(t, "z12", "rs2", nil)
	a := newDoItApp(t, ts, "4.50 1.50 1.00 3/100 12345", "z12", "rs2")
	a.doIt()
	if len(*rules) != 1 {
		t.Errorf("expected 1 rule when load equals maxLoad, got %d", len(*rules))
	}
}

func TestDoIt_HighLoadReplacesStaleRule(t *testing.T) {
	stale := testRule{ID: "stale", Description: botCheckDescription, Expression: "old expression"}
	ts, rules := rulesetServer(t, "z13", "rs2", []testRule{stale})
	a := newDoItApp(t, ts, "10.00 8.00 6.00 5/200 12345", "z13", "rs2")
	a.doIt()
	if len(*rules) != 1 {
		t.Fatalf("expected 1 rule after replace, got %d", len(*rules))
	}
	if (*rules)[0].ID == "stale" {
		t.Error("stale rule should have been replaced")
	}
}
