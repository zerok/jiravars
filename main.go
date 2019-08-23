package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	yaml "gopkg.in/yaml.v2"
)

type metricConfiguration struct {
	Name           string            `yaml:"name"`
	Help           string            `yaml:"help"`
	JQL            string            `yaml:"jql"`
	Interval       string            `yaml:"interval"`
	Labels         map[string]string `yaml:"labels"`
	ParsedInterval time.Duration
	Gauge          prometheus.Gauge
}

type configuration struct {
	BaseURL  string                `yaml:"baseURL"`
	Login    string                `yaml:"login"`
	Password string                `yaml:"password"`
	Metrics  []metricConfiguration `yaml:"metrics"`
}

func loadConfiguration(path string) (*configuration, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = ioutil.ReadAll(os.Stdin)
	} else {
		data, err = ioutil.ReadFile(path)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s", path)
	}
	cfg := &configuration{}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, errors.Wrap(err, "failed to parse config data")
	}

	for i := 0; i < len(cfg.Metrics); i++ {
		// Set a default value of 5 minutes if none has been specified.
		if cfg.Metrics[i].Interval == "" {
			cfg.Metrics[i].Interval = "5m"
		}
		dur, err := time.ParseDuration(cfg.Metrics[i].Interval)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid interval for metric %d", i)
		}
		cfg.Metrics[i].ParsedInterval = dur
	}
	return cfg, nil
}

type pagedResponse struct {
	Total uint64 `json:"total"`
}

func check(ctx context.Context, log *logrus.Logger, cfg *configuration, wg *sync.WaitGroup) {
	for idx, m := range cfg.Metrics {
		go func(idx int, m metricConfiguration) {
			timer := time.NewTicker(m.ParsedInterval)
			params := url.Values{}
			params.Set("jql", m.JQL)
			params.Set("maxResults", "0")
			u := fmt.Sprintf("%s/rest/api/2/search?%s", cfg.BaseURL, params.Encode())

			client := http.Client{}

			defer timer.Stop()
		loop:
			for {
				var resp *http.Response
				pr := pagedResponse{}
				log.Debugf("Checking %s", m.Name)
				r, err := http.NewRequest(http.MethodGet, u, nil)
				if err != nil {
					log.WithError(err).Errorf("Failed to create HTTP request with URL = %s", u)
					goto next
				}
				r.SetBasicAuth(cfg.Login, cfg.Password)
				resp, err = client.Do(r)
				if err != nil {
					log.WithError(err).WithField("url", u).Errorf("Failed to execute HTTP request")
					goto next
				}
				if resp.StatusCode != http.StatusOK {
					resp.Body.Close()
					log.WithField("url", u).Errorf("HTTP response had status %d instead of 200", resp.StatusCode)
					goto next
				}
				if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
					resp.Body.Close()
					log.WithError(err).WithField("url", u).Errorf("Failed to parse HTTP response")
					goto next
				}
				resp.Body.Close()
				cfg.Metrics[idx].Gauge.Set(float64(pr.Total))
				log.Debugf("Completed %s: %v", m.Name, pr.Total)
			next:
				select {
				case <-timer.C:
				case <-ctx.Done():
					break loop
				}
			}
			log.Infof("Stopping worker for %s", m.Name)
			defer wg.Done()
		}(idx, m)
	}
}

func setupGauges(metrics []metricConfiguration) error {
	for i := 0; i < len(metrics); i++ {
		metrics[i].Gauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        fmt.Sprintf("jira_%s", metrics[i].Name),
			ConstLabels: metrics[i].Labels,
			Help:        metrics[i].Help,
		})
		if err := prometheus.Register(metrics[i].Gauge); err != nil {
			return err
		}
	}
	return nil
}
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := logrus.New()
	var configFile string
	var addr string
	var verbose bool
	pflag.StringVar(&configFile, "config", "", "Path to a configuration file")
	pflag.StringVar(&addr, "http-addr", "127.0.0.1:9300", "Address the HTTP server should be listening on")
	pflag.BoolVar(&verbose, "verbose", false, "Verbose logging")
	pflag.Parse()

	if verbose {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.InfoLevel)
	}

	if configFile == "" {
		log.Fatal("Please specify a config file using --config CONFIG_FILE")
	}

	cfg, err := loadConfiguration(configFile)
	if err != nil {
		log.WithError(err).Fatalf("Failed to load config from %s", configFile)
	}

	if err := setupGauges(cfg.Metrics); err != nil {
		log.WithError(err).Fatal("Failed to setup gauges")
	}

	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, syscall.SIGINT)
	httpServer := http.Server{}

	wg := sync.WaitGroup{}
	wg.Add(len(cfg.Metrics) + 2)
	go func() {
		<-sigChan
		log.Info("Shutting down...")
		httpServer.Close()
		cancel()
		defer wg.Done()
	}()

	go check(ctx, log, cfg, &wg)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	httpServer.Handler = mux
	httpServer.Addr = addr

	go func() {
		defer wg.Done()
		log.Infof("Starting server on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil {
			cancel()
			log.WithError(err).Error("Server stopped")
		}
	}()

	wg.Wait()
}
