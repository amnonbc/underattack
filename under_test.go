package main

import (
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
