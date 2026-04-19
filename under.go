package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/go-ps"

	_ "github.com/go-sql-driver/mysql"
)

type Config struct {
	Domain     string
	ApiKey     string
	DbName     string
	DbUser     string
	DbPassword string
	RulesetID  string
}

type app struct {
	conf     Config
	maxLoad  float64
	minLoad  float64
	maxProcs int
	loadFile string
	zoneId   string
	client   *http.Client
	baseURL  string // override for testing; defaults to cloudflare base
}

func (a *app) loadConfig(fn string) error {
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&a.conf); err != nil {
		return err
	}
	var missing []string
	if a.conf.ApiKey == "" {
		missing = append(missing, "apiKey")
	}
	if a.conf.Domain == "" {
		missing = append(missing, "domain")
	}
	if a.conf.RulesetID == "" {
		missing = append(missing, "RulesetID")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config missing required fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

func loadAvg(text string) ([]float64, error) {
	var res []float64
	fields := strings.Fields(text)
	if len(fields) < 4 {
		return nil, errors.New("empty number")
	}
	for i, field := range fields {
		f, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return nil, err
		}
		res = append(res, f)
		if i >= 2 {
			break
		}
	}
	return res, nil
}

func (a *app) init() error {
	req, err := a.NewRequest(http.MethodGet, a.baseURL+"/zones", nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := decodeCF(resp, &zones); err != nil {
		return err
	}

	for _, z := range zones {
		if z.Name == a.conf.Domain {
			a.zoneId = z.ID
			return nil
		}
	}

	return errors.New("zone ID not found for domain " + a.conf.Domain)
}

func countProcesses(pattern string) (int, error) {
	procs, err := ps.Processes()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, proc := range procs {
		if proc.Executable() == pattern {
			n++
		}
	}
	return n, nil
}

func (a *app) NewRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.conf.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func buildExpression() string {
	now := time.Now()
	clauses := make([]string, 9)
	for i := range 9 {
		d := now.AddDate(0, 0, 1-i).Format("02-01-2006") // tomorrow through 7 days ago
		clauses[i] = fmt.Sprintf(`http.request.uri.path contains "/%s/"`, d)
	}
	return `http.request.uri.path contains "/articles/" and http.request.method eq "GET" and not cf.client.bot and not http.cookie contains "wordpress_logged_in"` +
		" and not (" + strings.Join(clauses, " or ") + ")"
}

const botCheckDescription = "Bot check"

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e cfError) Error() string {
	return fmt.Sprintf("cloudflare error %d: %s", e.Code, e.Message)
}

// decodeCF checks the HTTP status and decodes a Cloudflare JSON envelope into
// dst (the result field), returning an error if the status is non-2xx or
// success=false.
func decodeCF(resp *http.Response, dst any) error {
	defer resp.Body.Close()
	var env struct {
		Success bool      `json:"success"`
		Errors  []cfError `json:"errors"`
		Result  any       `json:"result"`
	}
	if dst != nil {
		env.Result = dst
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("HTTP %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode/100 != 2 {
		if len(env.Errors) > 0 {
			return env.Errors[0]
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !env.Success {
		if len(env.Errors) > 0 {
			return env.Errors[0]
		}
		return fmt.Errorf("cloudflare API returned success=false")
	}
	return nil
}

type ruleInfo struct {
	ID         string
	Expression string
}

// findRule returns the bot check rule's ID and expression, or nil if it doesn't exist.
func (a *app) findRule() (*ruleInfo, error) {
	url := fmt.Sprintf(a.baseURL+"/zones/%s/rulesets/%s", a.zoneId, a.conf.RulesetID)
	req, err := a.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}

	var data struct {
		Rules []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Expression  string `json:"expression"`
		} `json:"rules"`
	}
	if err := decodeCF(resp, &data); err != nil {
		return nil, err
	}
	for _, r := range data.Rules {
		if r.Description == botCheckDescription {
			return &ruleInfo{ID: r.ID, Expression: r.Expression}, nil
		}
	}
	return nil, nil
}

func (a *app) createRule(reason string) error {
	url := fmt.Sprintf(a.baseURL+"/zones/%s/rulesets/%s/rules", a.zoneId, a.conf.RulesetID)
	payload := map[string]any{
		"action":      "managed_challenge",
		"description": botCheckDescription,
		"enabled":     true,
		"expression":  buildExpression(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := a.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	var result struct {
		Rules []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Expression  string `json:"expression"`
		} `json:"rules"`
	}
	if err := decodeCF(resp, &result); err != nil {
		return err
	}
	for _, r := range result.Rules {
		if r.Description == botCheckDescription {
			ruleURL := fmt.Sprintf(a.baseURL+"/zones/%s/rulesets/%s/rules/%s", a.zoneId, a.conf.RulesetID, r.ID)
			slog.Info("created bot check rule", "reason", reason, "id", r.ID, "url", ruleURL)
			slog.Debug("bot check rule details", "description", r.Description, "expression", r.Expression)
			return nil
		}
	}
	slog.Info("created bot check rule (id unknown)", "reason", reason)
	return nil
}

func (a *app) deleteRule(ruleID string) error {
	url := fmt.Sprintf(a.baseURL+"/zones/%s/rulesets/%s/rules/%s", a.zoneId, a.conf.RulesetID, ruleID)
	req, err := a.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	if err := decodeCF(resp, nil); err != nil {
		return err
	}
	slog.Info("deleted bot check rule", "id", ruleID)
	return nil
}

// ensureBotCheck creates the bot check rule (active=true) or removes it (active=false).
// When activating, the rule is only replaced if today's date is not already in the
// expression — avoiding churn on every run while the server stays under load.
// reason is logged alongside creation to explain why it was triggered.
func (a *app) ensureBotCheck(active bool, reason string) error {
	info, err := a.findRule()
	if err != nil {
		return fmt.Errorf("finding bot check rule: %w", err)
	}
	if active {
		today := time.Now().Format("02-01-2006")
		if info != nil && strings.Contains(info.Expression, today) {
			slog.Info("bot check rule already current, skipping", "id", info.ID, "reason", reason)
			return nil
		}
		if info != nil {
			if err := a.deleteRule(info.ID); err != nil {
				return err
			}
			if reason == "" {
				reason = "date rollover"
			}
		}
		return a.createRule(reason)
	}
	if info != nil {
		return a.deleteRule(info.ID)
	}
	return nil
}

func newApp() *app {
	return &app{
		client:  http.DefaultClient,
		baseURL: "https://api.cloudflare.com/client/v4",
	}
}

func main() {
	a := newApp()

	cf := flag.String("config", "/etc/botCheck.conf", "config file")
	flag.BoolFunc("debug", "enable debug logging", func(string) error {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
		return nil
	})
	flag.Float64Var(&a.maxLoad, "maxLoad", 4.5, "max load before enabling bot check rule")
	flag.Float64Var(&a.minLoad, "minLoad", 1.0, "disable bot check rule if load is this low")
	flag.IntVar(&a.maxProcs, "maxProc", 20, "max number of lsphp processes we allow to run")
	flag.StringVar(&a.loadFile, "loadFile", "/proc/loadavg", "location of loadavg proc file")
	flag.Parse()

	if err := a.loadConfig(*cf); err != nil {
		slog.Error("loading config", "err", err)
		os.Exit(1)
	}

	if err := a.init(); err != nil {
		slog.Error("initialising", "err", err)
		os.Exit(1)
	}

	a.doIt()
}

func (a *app) doIt() {
	text, err := os.ReadFile(a.loadFile)
	if err != nil {
		slog.Error("reading load file", "err", err)
		os.Exit(1)
	}

	la, err := loadAvg(string(text))
	if err != nil {
		slog.Error("parsing load average", "err", err)
		os.Exit(1)
	}

	if err := a.checkDb(); err != nil {
		slog.Warn("cannot connect to db, enabling bot check rule", "err", err)
		if err := a.ensureBotCheck(true, "db unavailable"); err != nil {
			slog.Error("failed to enable bot check rule", "err", err)
			os.Exit(1)
		}
		return
	}

	lsphpCount, err := countProcesses("lsphp")
	if err != nil {
		slog.Warn("could not count lsphp processes", "err", err)
	} else if lsphpCount > a.maxProcs {
		slog.Info("lsphp count above threshold, enabling bot check rule", "count", lsphpCount)
		if err := a.ensureBotCheck(true, fmt.Sprintf("lsphp count %d", lsphpCount)); err != nil {
			slog.Error("failed to enable bot check rule", "err", err)
			os.Exit(1)
		}
		return
	}

	if la[0] >= a.maxLoad {
		slog.Debug("load average above threshold, enabling bot check rule", "load", la[0])
		if err := a.ensureBotCheck(true, fmt.Sprintf("load %.2f", la[0])); err != nil {
			slog.Error("failed to enable bot check rule", "err", err)
			os.Exit(1)
		}
		return
	}

	if allBelow(la, a.minLoad) {
		slog.Debug("load average below threshold, disabling bot check rule", "load", la[0])
		if err := a.ensureBotCheck(false, ""); err != nil {
			slog.Error("failed to disable bot check rule", "err", err)
			os.Exit(1)
		}
	}
}

func allBelow(a []float64, x float64) bool {
	return !slices.ContainsFunc(a, func(v float64) bool { return v >= x })
}
