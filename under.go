package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

type Config struct {
	Domain     string
	ApiKey     string
	DbName     string
	DbUser     string
	DbPassword string
	RuleID     string
	RulesetID  string
}

type app struct {
	conf     Config
	maxLoad  float64
	minLoad  float64
	loadFile string
	zoneId   string
}

func (a *app) loadConfig(fn string) error {
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&a.conf)
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
	zoneResp, err := http.NewRequest("GET", "https://api.cloudflare.com/client/v4/zones", nil)
	if err != nil {
		return err
	}
	zoneResp.Header.Set("Authorization", "Bearer "+a.conf.ApiKey)
	zoneResp.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(zoneResp)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var data struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return err
	}

	for _, z := range data.Result {
		if z.Name == a.conf.Domain {
			a.zoneId = z.ID
			return nil
		}
	}

	return errors.New("zone ID not found for domain " + a.conf.Domain)
}

func countLsphpProcesses() (int, error) {
	out, err := exec.Command("pgrep", "-fc", "lsphp").Output()
	if err != nil {
		return 0, err
	}
	countStr := strings.TrimSpace(string(out))
	return strconv.Atoi(countStr)
}

// getRuleState fetches the current enabled state of the rule
func (a *app) getRuleState() (bool, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/rulesets/%s",
		a.zoneId, a.conf.RulesetID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("Authorization", "Bearer "+a.conf.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("Cloudflare API returned HTTP %d: %s", resp.StatusCode, respBody)
	}

	var data struct {
		Result struct {
			Rules []struct {
				ID      string `json:"id"`
				Enabled bool   `json:"enabled"`
			} `json:"rules"`
		} `json:"result"`
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return false, err
	}

	for _, rule := range data.Result.Rules {
		if rule.ID == a.conf.RuleID {
			return rule.Enabled, nil
		}
	}

	return false, errors.New("rule not found in ruleset")
}

func (a *app) setRuleEnabled(enable bool) error {
	// Check current state only when we need to make a change
	currentState, err := a.getRuleState()
	if err != nil {
		log.Printf("Warning: could not fetch current rule state: %v", err)
		log.Println("Proceeding with rule update anyway...")
	} else if currentState == enable {
		// log.Printf("Rule is already %s, skipping API call", map[bool]string{true: "enabled", false: "disabled"}[enable])
		return nil
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/rulesets/%s/rules/%s",
		a.zoneId, a.conf.RulesetID, a.conf.RuleID)

	payload := map[string]interface{}{
		"action":      "managed_challenge",
		"description": "Bot check",
		"enabled":     enable,
		"expression":  "http.request.uri.path contains \"/articles/\" and http.request.method eq \"GET\" and not cf.client.bot and not http.cookie contains \"wordpress_logged_in\"",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.conf.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Cloudflare API returned HTTP %d: %s", resp.StatusCode, respBody)
	}

	log.Printf("Successfully %s bot check rule", map[bool]string{true: "enabled", false: "disabled"}[enable])

	return nil
}

func main() {
	var a app

	log.SetFlags(log.LstdFlags)

	cf := flag.String("config", "/etc/botCheck.conf", "config file")
	flag.Float64Var(&a.maxLoad, "maxLoad", 4.5, "max load before enabling bot check rule")
	flag.Float64Var(&a.minLoad, "minLoad", 1.0, "disable bot check rule if load is this low")
	flag.StringVar(&a.loadFile, "loadFile", "/proc/loadavg", "location of loadavg proc file")
	flag.Parse()

	if err := a.loadConfig(*cf); err != nil {
		log.Fatalln(err)
	}

	if err := a.init(); err != nil {
		log.Fatalln(err)
	}

	a.doIt()
}

func (a *app) doIt() {
	text, err := os.ReadFile(a.loadFile)
	if err != nil {
		log.Fatalln(err)
	}

	la, err := loadAvg(string(text))
	if err != nil {
		log.Fatalln(err)
	}

	err = a.checkDb()
	if err != nil {
		log.Println("cannot connect to db:", err)
		log.Println("enabling Cloudflare bot check rule due to DB failure")
		err := a.setRuleEnabled(true)
		if err != nil {
			log.Fatalln("Failed to enable bot check rule:", err)
		}
		return
	}

	lsphpCount, err := countLsphpProcesses()
	if err != nil {
		log.Println("Warning: could not count lsphp processes:", err)
	} else {
		// log.Println("lsphp process count:", lsphpCount)
		if lsphpCount > 20 {
			log.Println("lsphp count:", lsphpCount, "- enabling bot check rule")
			err := a.setRuleEnabled(true)
			if err != nil {
				log.Fatalln("Failed to enable bot check rule:", err)
			}
			return
		}
	}

	if la[0] >= a.maxLoad {
		log.Println("Load average is", la, "enabling bot check rule")
		err := a.setRuleEnabled(true)
		if err != nil {
			log.Fatalln("Failed to enable bot check rule:", err)
		}
		return
	}

	if allBelow(la, a.minLoad) {
		// log.Println("Load average is below threshold, disabling bot check rule")
		err := a.setRuleEnabled(false)
		if err != nil {
			log.Fatalln("Below threshold but failed to disable bot check rule:", err)
		}
		return
	}
}

func allBelow(a []float64, x float64) bool {
	for _, v := range a {
		if v >= x {
			return false
		}
	}
	return true
}
