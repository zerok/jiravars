package main

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestSetupGauges(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := []metricConfiguration{
		{
			Name: "test_name",
			Help: "some help",
		},
	}
	require.NoError(t, setupGauges(reg, metrics))
	families, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	fam := families[0]
	require.Equal(t, "jira_test_name", *fam.Name)
	require.Equal(t, "some help", *fam.Help)
	require.Equal(t, "GAUGE", fam.Type.Enum().String())
}
