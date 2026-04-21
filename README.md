# underattack
Switch a Cloudflare WAF rule to challenge bots when server load is high.

[![Go](https://github.com/amnonbc/underattack/actions/workflows/go2.yml/badge.svg)](https://github.com/amnonbc/underattack/actions/workflows/go2.yml)

## Overview

This runs on a web host sitting behind Cloudflare. It is invoked by cron every
minute and checks the server's load average, free memory, and database
connectivity. If the server is under stress it creates a Cloudflare WAF rule
that issues a managed challenge to bot traffic, which typically allows server
resources to recover. Once the server recovers the rule is deleted.

## How It Works

Recent articles are cached by Cloudflare's edge and serve without hitting the
origin. The real load problem comes from **archive crawlers**: bots that
systematically traverse years of older content, each request hitting the
uncached origin. A single crawler can generate tens of thousands of origin
requests over hours or days.

The rule challenges GET requests to `/articles/` paths while exempting:
- Requests Cloudflare already identifies as known bots (`cf.client.bot`)
- Logged-in WordPress users (those with a `wordpress_logged_in` cookie)
- Articles published in the last 7 days (plus tomorrow for timezone coverage)

This preserves unfettered access to recent, cached, high-value content while
forcing archive crawlers to pace slowly through uncached older articles. The
crawler either gives up or spends hours proving it's not a bot, giving your
origin server time to recover.

The rule is created fresh on each activation so the exempted date window stays
current. If the rule already covers today's date it is left unchanged to avoid
unnecessary API churn.

## Usage

```
*/5 * * * * ${HOME}/bin/underattack -config ${HOME}/etc/underattack.conf >> ${HOME}/logs/underattack.log 2>&1
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `/etc/botCheck.conf` | Path to config file |
| `-maxLoad` | `4.5` | Enable bot check rule if 1-minute load average exceeds this |
| `-minLoad` | `1.0` | Remove bot check rule when all load averages drop below this |
| `-maxProc` | `20` | Enable bot check rule if lsphp process count exceeds this |
| `-loadFile` | `/proc/loadavg` | Path to load average file |
| `-exemptDays` | `9` | Number of days to exempt from the bot check (includes tomorrow) |
| `-dateFormat` | `02-01-2006` | Go time format used for dates in article URLs |
| `-debug` | off | Enable debug logging |

## Config file

```json
{
    "domain": "mydomain.com",
    "apiKey": "cfApiKeyWithZoneReadAndZoneSecurityEditPermissions",
    "RulesetID": "yourCloudflareRulesetID",
    "DbName": "nameOfDb",
    "DbUser": "nameOfDbUser",
    "DbPassword": "dbUserPassword"
}
```

The Cloudflare API key requires **Zone:Read** and **Zone WAF:Edit** permissions.
The ruleset ID can be found via `GET /zones/{zone_id}/rulesets` or in the
Cloudflare dashboard under Security → WAF → Custom Rules.

## Monitoring

When `MetricsURL` and `MetricsToken` are configured, the tool pushes the following
metrics to Grafana Cloud every 5 minutes (via cron):
- `bot_check_rule_enabled` (0 or 1)
- `load_average` (1-minute load)
- `memory_percent` (0-100)
- `php_process_count`

View the dashboard at:

https://amnonbc.grafana.net/d/bot-check-dashboard/bot-check-rule-status?orgId=1&from=now-3h&to=now&timezone=UTC

The dashboard displays:
- **Bot Check Rule**: Daily and hourly percentage enabled
- **CPU Load Average**: 1-minute load average trend
- **Memory Usage**: Memory utilization percentage
- **PHP Process Count**: Active lsphp worker processes

## Cross-compiling for Linux

```
GOOS=linux GOARCH=amd64 go build -o underattack .
```