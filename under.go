package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/cloudflare/cloudflare-go"
)

type Config struct {
	Domain string
	ApiKey string
}

var config Config

func loadConfig(fn string) error {
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&config)
}

func loadAvg(text string) (float64, error) {
	f := strings.Fields(text)
	if len(f) == 0 {
		return 0, errors.New("empty number")
	}
	return strconv.ParseFloat(f[0], 64)
}

// setSecurityLevel sets the security level. value must be one of
// off, essentially_off, low, medium, high, under_attack.
func setSecurityLevel(value string) error {
	api, err := cloudflare.NewWithAPIToken(config.ApiKey)
	if err != nil {
		return err
	}
	zoneID, err := api.ZoneIDByName(config.Domain)
	if err != nil {
		return err
	}

	settings, err := api.ZoneSettings(context.TODO(), zoneID)
	if err != nil {
		return err
	}
	const securityLevel = "security_level"

	for _, s := range settings.Result {
		if s.ID != securityLevel {
			continue
		}
		if s.Value.(string) == value {
			log.Println(securityLevel, "already is", value)
			return nil
		}
	}

	log.Println("setting security level to", value)
	_, err = api.UpdateZoneSettings(context.TODO(), zoneID, []cloudflare.ZoneSetting{
		{
			ID:    securityLevel,
			Value: value,
		},
	})
	return err
}

func main() {
	cf := flag.String("config", "/etc/underattack.conf", "config file")
	maxLoad := flag.Float64("maxLoad", 6.0, "max load before going into lockdown")
	minLoad := flag.Float64("minLoad", 2.0, "turn down to medium if we reach this level")
	defaultSecurityLevel := flag.String("default_level", "medium", "sercurity level to set when load is low")
	loadFile := flag.String("loadFile", "/proc/loadavg", "location of loadavg proc file")
	flag.Parse()
	err := loadConfig(*cf)
	if err != nil {
		log.Fatalln(err)
	}

	text, err := os.ReadFile(*loadFile)
	if err != nil {
		log.Fatalln(err)
	}
	la, err := loadAvg(string(text))
	if err != nil {
		log.Fatalln(err)
	}
	log.Println("Load average is", la)
	if la >= *maxLoad {
		err = setSecurityLevel("under_attack")
	}
	if la <= *minLoad {
		err = setSecurityLevel(*defaultSecurityLevel)
	}
	if err != nil {
		log.Println(err)
	}
}
