# underattack
switch cloudflare FW to under_attack mode when server load is high

[![Go](https://github.com/amnonbc/underattack/actions/workflows/go2.yml/badge.svg)](https://github.com/amnonbc/underattack/actions/workflows/go2.yml)

This runs on a web host which sits behind cloudflare.

It is run using a cron like

```
*/5 * * * * ${HOME}/bin/underattack -config ${HOME}/etc/underatttack.conf -default_level high >> ${HOME}/logs/underattack.log 2>&1
```

It checks the server's load, free memory and tries to connect to the db.
If the load is too high, the free memory is too low, or if it can't connect to the db,
then it sets CF into under-attack mode, which massively reduces bot traffic which usually allows server resources to recover.
Once the server has recovered, then it turns off under-attack mode.

The config file looks like this:

```json
{
	"domain": "mydomain.com",
	"apiKey": "cfApiKeyWithZoneZoneReadAmdZoneSecuritySet",
	"DbName": "nameOfDb",
	"DbUser": "NameOfDbUser",
	"DbPassword": "dbUserPassword"
}
```