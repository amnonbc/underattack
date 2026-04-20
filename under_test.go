package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
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
	a := newApp()
	a.maxLoad = 4.5
	a.minLoad = 1.0
	a.maxProcs = 9999
	a.client = ts.Client()
	a.baseURL = ts.URL
	a.zoneId = zoneID
	a.conf = Config{ApiKey: "test-key", RulesetID: rulesetID}
	return a
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

func TestCfURL(t *testing.T) {
	a := &app{baseURL: "https://api.cloudflare.com/client/v4"}
	cases := []struct {
		segments []string
		want     string
	}{
		{[]string{"zones"}, "https://api.cloudflare.com/client/v4/zones"},
		{[]string{"zones", "zoneID", "rulesets", "rulesetID"}, "https://api.cloudflare.com/client/v4/zones/zoneID/rulesets/rulesetID"},
		{[]string{"zones", "zoneID", "rulesets", "rulesetID", "rules"}, "https://api.cloudflare.com/client/v4/zones/zoneID/rulesets/rulesetID/rules"},
		{[]string{"zones", "zoneID", "rulesets", "rulesetID", "rules", "ruleID"}, "https://api.cloudflare.com/client/v4/zones/zoneID/rulesets/rulesetID/rules/ruleID"},
	}
	for _, tc := range cases {
		if got := a.cfURL(tc.segments...); got != tc.want {
			t.Errorf("cfURL(%v) = %q, want %q", tc.segments, got, tc.want)
		}
	}
}

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

func TestBuildExpression(t *testing.T) {
	now := date(2026, 4, 20)
	a := newApp()

	cases := []struct {
		name       string
		exemptDays int
		want       string
	}{
		{
			name:       "zero exempt days — no matches clause",
			exemptDays: 0,
			want:       `http.request.uri.path contains "/articles/" and http.request.method eq "GET" and not cf.client.bot and not http.cookie contains "wordpress_logged_in"`,
		},
		{
			name:       "one exempt day — matches tomorrow",
			exemptDays: 1,
			want:       `http.request.uri.path contains "/articles/" and http.request.method eq "GET" and not cf.client.bot and not http.cookie contains "wordpress_logged_in" and not http.request.uri.path matches "/21-04-2026/"`,
		},
		{
			name:       "nine exempt days — matches clause covers window",
			exemptDays: 9,
			want:       `http.request.uri.path contains "/articles/" and http.request.method eq "GET" and not cf.client.bot and not http.cookie contains "wordpress_logged_in" and not http.request.uri.path matches "/(21|20|19|18|17|16|15|14|13)-04-2026/"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a.exemptDays = tc.exemptDays
			got := a.buildExpression(now)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// exemptExpression — corner cases with fixed dates
// ---------------------------------------------------------------------------

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func articlePath(d time.Time) string {
	return "/articles/123456/" + d.Format("02-01-2006") + "/some-slug"
}

func checkRegex(t *testing.T, re *regexp.Regexp, lastDay time.Time, days int) {
	t.Helper()
	for i := range days {
		d := lastDay.AddDate(0, 0, -i)
		if !re.MatchString(articlePath(d)) {
			t.Errorf("regex should match %s (day %d of window)", d.Format("02-01-2006"), i)
		}
	}
	// Day after window (newer than lastDay)
	if after := lastDay.AddDate(0, 0, 1); re.MatchString(articlePath(after)) {
		t.Errorf("regex should not match %s (day after window)", after.Format("02-01-2006"))
	}
	// Day before window (older than oldest exempt date)
	if before := lastDay.AddDate(0, 0, -days); re.MatchString(articlePath(before)) {
		t.Errorf("regex should not match %s (day before window)", before.Format("02-01-2006"))
	}
}

func TestExemptExpression(t *testing.T) {
	cases := []struct {
		name     string
		lastDay  time.Time
		days     int
		want     string
		contains []string // for cases where exact match is impractical
	}{
		{
			name:    "zero days returns empty",
			lastDay: date(2026, 4, 20),
			days:    0,
			want:    "",
		},
		{
			name:    "one day",
			lastDay: date(2026, 4, 20),
			days:    1,
			want:    `/20-04-2026/`,
		},
		{
			name:    "multiple days same month",
			lastDay: date(2026, 4, 21),
			days:    3,
			want:    `/(21|20|19)-04-2026/`,
		},
		{
			name:    "month boundary multiple days each side",
			lastDay: date(2026, 4, 2),
			days:    4,
			want:    `/((02|01)-04-2026|(31|30)-03-2026)/`,
		},
		{
			name:    "month boundary one day each side",
			lastDay: date(2026, 4, 1),
			days:    2,
			want:    `/(01-04-2026|31-03-2026)/`,
		},
		{
			name:    "year boundary",
			lastDay: date(2026, 1, 1),
			days:    3,
			want:    `/(01-01-2026|(31|30)-12-2025)/`,
		},
		{
			name:    "leap day",
			lastDay: date(2028, 3, 1),
			days:    3,
			want:    `/(01-03-2028|(29|28)-02-2028)/`,
		},
		{
			name:    "non-leap year feb",
			lastDay: date(2026, 3, 1),
			days:    3,
			want:    `/(01-03-2026|(28|27)-02-2026)/`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := exemptExpression(tc.lastDay, tc.days)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if tc.days == 0 {
				return
			}
			re, err := regexp.Compile(got)
			if err != nil {
				t.Fatalf("invalid regex %q: %v", got, err)
			}
			checkRegex(t, re, tc.lastDay, tc.days)
		})
	}
}

func TestExemptExpression_SpansThreeMonths(t *testing.T) {
	// Mar 1 back 32 days: spans January, February, and March 2026.
	lastDay := date(2026, 3, 1)
	got := exemptExpression(lastDay, 32)
	for _, want := range []string{"03-2026", "02-2026", "01-2026"} {
		if !strings.Contains(got, want) {
			t.Errorf("result missing %q: %s", want, got)
		}
	}
	re := regexp.MustCompile(got)
	checkRegex(t, re, lastDay, 32)
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
