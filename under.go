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

type app struct {
	conf Config

	maxLoad              float64
	minLoad              float64
	minFreeBytes         uint64
	defaultSecurityLevel string
	loadFile             string

	api    *cloudflare.API
	zoneId string
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
	var err error
	a.api, err = cloudflare.NewWithAPIToken(a.conf.ApiKey)
	if err != nil {
		return err
	}
	a.zoneId, err = a.api.ZoneIDByName(a.conf.Domain)
	return err
}

// setSecurityLevel sets the security level. value must be one of
// off, essentially_off, low, medium, high, under_attack.
func (a *app) setSecurityLevel(value string) error {
	currentLevel, err := a.currentLevel()
	if err == nil && currentLevel == value {
		// Nothing to do.
		return nil
	}

	log.Println("setting security level to", value)
	_, err = a.api.UpdateZoneSettings(context.TODO(), a.zoneId, []cloudflare.ZoneSetting{
		{
			ID:    securityLevel,
			Value: value,
		},
	})
	return err
}

func (a *app) mustSetSecurityLevel(value string) {
	err := a.setSecurityLevel(value)
	if err != nil {
		log.Fatalln(err)
	}
}

func (a *app) currentLevel() (string, error) {
	settings, err := a.api.ZoneSettings(context.TODO(), a.zoneId)
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
	var a app
	cf := flag.String("config", "/etc/underattack.conf", "config file")
	flag.Float64Var(&a.maxLoad, "maxLoad", 6.0, "max load before going into lockdown")
	flag.Float64Var(&a.minLoad, "minLoad", 1.0, "turn down to medium if we reach this level")
	minBytesStr := flag.String("minBytes", "1 GB", "go into lockdown if free memory falls below minBytes")
	flag.StringVar(&a.defaultSecurityLevel, "default_level", "medium", "sercurity level to set when load is low")
	flag.StringVar(&a.loadFile, "loadFile", "/proc/loadavg", "location of loadavg proc file")
	flag.Parse()
	var err error
	a.minFreeBytes, err = humanize.ParseBytes(*minBytesStr)
	if err != nil {
		log.Fatalln(err)
	}
	err = a.loadConfig(*cf)
	if err != nil {
		log.Fatalln(err)
	}

	err = a.init()
	if err != nil {
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
	freeMem := memory.FreeMemory()
	log.Println("freeMem", humanize.Bytes(freeMem), "load", la)
	if freeMem < a.minFreeBytes {
		log.Println("free memory is below", humanize.Bytes(a.minFreeBytes))
		a.mustSetSecurityLevel("under_attack")
		return
	}
	err = a.checkDb()
	if err != nil {
		log.Println("can not connect to db", err)
		a.mustSetSecurityLevel("under_attack")
		return
	}

	if la[0] >= a.maxLoad {
		log.Println("Load average is", la, "setting level to under_attack")
		a.mustSetSecurityLevel("under_attack")
		return
	}
	if allBelow(la, a.minLoad) {
		a.mustSetSecurityLevel(a.defaultSecurityLevel)
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
