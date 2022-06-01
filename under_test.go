package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_loadAvg(t *testing.T) {
	got, err := loadAvg("1.01 0.97 0.94 1/159 2795695")
	require.NoError(t, err)
	assert.Equal(t, 1.01, got)
}

func Test_loadAvgNil(t *testing.T) {
	_, err := loadAvg("")
	require.Error(t, err)
}

func Test_loadAvgNan(t *testing.T) {
	_, err := loadAvg("abc")
	require.Error(t, err)
}
