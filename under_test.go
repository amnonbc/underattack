package main

import (
	"encoding/json"
	"github.com/cloudflare/cloudflare-go"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_loadAvg(t *testing.T) {
	got, err := loadAvg("1.01 0.97 0.94 1/159 2795695")
	require.NoError(t, err)
	assert.Equal(t, []float64{1.01, 0.97, 0.94}, got)
}

func Test_loadAvgNil(t *testing.T) {
	_, err := loadAvg("")
	require.Error(t, err)
}

func Test_loadAvgNan(t *testing.T) {
	_, err := loadAvg("abc")
	require.Error(t, err)
}

const settingsResponse = `
{
  "result": [
    {
      "id": "security_level",
      "value": "medium"
    }
  ]
}
`

func Test_app_currentLevel(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/zones/myzone/settings", r.RequestURI)
		io.WriteString(w, settingsResponse)
	}))
	defer s.Close()
	a := app{zoneId: "myzone"}
	var err error
	a.api, err = cloudflare.New("key", "email")
	require.NoError(t, err)
	a.api.BaseURL = s.URL
	got, err := a.currentLevel()
	require.NoError(t, err)
	assert.Equal(t, "medium", got)
}

type updateSettings struct {
	Items []cloudflare.ZoneSetting `json:"items"`
}

func Test_app_setSecurityLevel(t *testing.T) {
	patched := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			io.WriteString(w, settingsResponse)
		case "PATCH":
			patched = true
			var us updateSettings
			err := json.NewDecoder(r.Body).Decode(&us)
			require.NoError(t, err)
			require.Equal(t, 1, len(us.Items))
			assert.Equal(t, "security_level", us.Items[0].ID)
			assert.Equal(t, "under_attack", us.Items[0].Value)
			io.WriteString(w, `{
			  "success": true,
			  "errors": [],
			  "messages": []
			}`)
		default:
			t.Errorf("There should not have been a %s request", r.Method)
		}
	}))
	defer s.Close()

	a := app{
		zoneId: "myzone",
	}
	var err error
	a.api, err = cloudflare.New("key", "email")
	require.NoError(t, err)
	a.api.BaseURL = s.URL
	err = a.setSecurityLevel("under_attack")
	assert.NoError(t, err)
	assert.True(t, patched)
}

func Test_app_setSecurityLevelAlreadyThere(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			io.WriteString(w, settingsResponse)
		default:
			t.Errorf("There should not have been a %s request", r.Method)
		}
	}))
	defer s.Close()

	a := app{
		zoneId: "myzone",
	}
	var err error
	a.api, err = cloudflare.New("key", "email")
	require.NoError(t, err)
	a.api.BaseURL = s.URL
	err = a.setSecurityLevel("medium")
	assert.NoError(t, err)
}
