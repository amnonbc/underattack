package main

import (
	"os"
	"testing"
	"time"
)

// TestIntegration_ExpressionAccepted creates the bot check rule against the real
// Cloudflare API and verifies it is accepted, then deletes it immediately.
//
// Requires environment variables:
//
//	CF_API_KEY   — Cloudflare API key with Zone:Read and Zone WAF:Edit permissions
//	CF_DOMAIN    — domain name (e.g. socialistparty.org.uk)
//	CF_RULESET_ID — ID of the custom rules ruleset for the zone
func TestIntegration_ExpressionAccepted(t *testing.T) {
	apiKey := os.Getenv("CF_API_KEY")
	domain := os.Getenv("CF_DOMAIN")
	rulesetID := os.Getenv("CF_RULESET_ID")
	if apiKey == "" || domain == "" || rulesetID == "" {
		t.Skip("set CF_API_KEY, CF_DOMAIN, CF_RULESET_ID to run integration tests")
	}

	a := newApp()
	a.conf.ApiKey = apiKey
	a.conf.Domain = domain
	a.conf.RulesetID = rulesetID

	if err := a.getZoneID(); err != nil {
		t.Fatalf("getZoneID: %v", err)
	}

	// Delete any leftover rule from a previous failed test run.
	if info, err := a.findRule(); err != nil {
		t.Fatalf("findRule (pre-clean): %v", err)
	} else if info != nil {
		if err := a.deleteRule(info.ID); err != nil {
			t.Fatalf("deleteRule (pre-clean): %v", err)
		}
	}

	if err := a.createRule("integration test"); err != nil {
		t.Fatalf("createRule: %v — expression may be invalid", err)
	}

	t.Cleanup(func() {
		info, err := a.findRule()
		if err != nil {
			t.Logf("cleanup findRule: %v", err)
			return
		}
		if info != nil {
			if err := a.deleteRule(info.ID); err != nil {
				t.Logf("cleanup deleteRule: %v", err)
			}
		}
	})

	info, err := a.findRule()
	if err != nil {
		t.Fatalf("findRule after create: %v", err)
	}
	if info == nil {
		t.Fatal("rule not found after creation")
	}
	t.Logf("rule accepted: id=%s", info.ID)
	t.Logf("expression: %s", info.Expression)
	t.Logf("expression length: %d / 4096 chars", len(a.buildExpression(time.Now())))
}
