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

const securityLevel = "security_level"

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

	currentLevel, err := currentLevel(api, zoneID)
	if currentLevel == value {
		return nil
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

func currentLevel(api *cloudflare.API, zoneID string) (string, error) {
	settings, err := api.ZoneSettings(context.TODO(), zoneID)
	if err != nil {
		return "", err
	}

	for _, s := range settings.Result {
		if s.ID == securityLevel {
			return s.Value.(string), nil
		}
	}
	return "", nil
}

func main() {
	cf := flag.String("config", "/etc/underattack.conf", "config file")
	maxLoad := flag.Float64("maxLoad", 6.0, "max load before going into lockdown")
	minLoad := flag.Float64("minLoad", 1.0, "turn down to medium if we reach this level")
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
	if la[0] >= *maxLoad {
		log.Println("Load average is", la, "setting level to under_attack")
		err = setSecurityLevel("under_attack")
	}
	if la[0] < *minLoad && la[1] < *minLoad && la[2] < *minLoad {
		err = setSecurityLevel(*defaultSecurityLevel)
	}
	if err != nil {
		log.Println(err)
	}
}
