package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	prom_dto "github.com/prometheus/client_model/go"
	"github.com/sirupsen/logrus"
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

func TestCheck(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// With no metrics defines, don't do anything but end if the context is
	// cancelled.
	t.Run("no-metrics", func(t *testing.T) {
		httpClient := &http.Client{}
		ctx, cancel := context.WithCancel(context.Background())
		cfg := &configuration{
			BaseURL:  "",
			Login:    "login",
			Password: "password",
		}
		go func() {
			time.Sleep(time.Second)
			cancel()
		}()
		check(ctx, log, cfg, httpClient)
	})

	// Test the happy case where we get data back from the server.
	t.Run("working-metric", func(t *testing.T) {
		httpClient := &http.Client{}
		reg := prometheus.NewRegistry()
		ctx, cancel := context.WithCancel(context.Background())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"total": 5}`)
			cancel()
		}))
		defer srv.Close()
		cfg := &configuration{
			BaseURL:  srv.URL,
			Login:    "login",
			Password: "password",
			Metrics: []metricConfiguration{
				{
					Name:           "test",
					Help:           "test",
					JQL:            "project = TEST",
					ParsedInterval: time.Second,
				},
			},
		}
		require.NoError(t, setupGauges(reg, cfg.Metrics))
		check(ctx, log, cfg, httpClient)
		results := make(chan prometheus.Metric, 2)
		cfg.Metrics[0].Gauge.Collect(results)
		result := <-results
		val := prom_dto.Metric{}
		result.Write(&val)
		require.Equal(t, float64(5), *val.Gauge.Value)
	})
}
