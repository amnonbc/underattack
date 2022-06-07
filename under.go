package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/cloudflare/cloudflare-go"
	"github.com/dustin/go-humanize"
	"github.com/pbnjay/memory"
	"log"
	"os"
	"strconv"
	"strings"
)

const securityLevel = "security_level"

type Config struct {
	Domain     string
	ApiKey     string
	DbName     string
	DbUser     string
	DbPassword string
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

func mustSetSecurityLevel(value string) {
	err := setSecurityLevel(value)
	if err != nil {
		log.Fatalln(err)
	}
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
	minBytesStr := flag.String("minBytes", "1 GB", "go into lockdown if free memory falls below minBytes")
	defaultSecurityLevel := flag.String("default_level", "medium", "sercurity level to set when load is low")
	loadFile := flag.String("loadFile", "/proc/loadavg", "location of loadavg proc file")
	flag.Parse()
	mb, err := humanize.ParseBytes(*minBytesStr)
	if err != nil {
		log.Fatalln(err)
	}
	err = loadConfig(*cf)
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
	freeMem := memory.FreeMemory()
	log.Println("freeMem", humanize.Bytes(freeMem), "load", la)
	if freeMem < mb {
		log.Println("free memory is below", *minBytesStr)
		mustSetSecurityLevel("under_attack")
		return
	}
	err = checkDb(config)
	if err != nil {
		log.Println("checkDb returned", err)
		mustSetSecurityLevel("under_attack")
		return
	}

	if la[0] >= *maxLoad {
		log.Println("Load average is", la, "setting level to under_attack")
		mustSetSecurityLevel("under_attack")
		return
	}
	if la[0] < *minLoad && la[1] < *minLoad && la[2] < *minLoad {
		mustSetSecurityLevel(*defaultSecurityLevel)
		return
	}
}
