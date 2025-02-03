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

// Define Jira API response types at the top
type Component struct {
	Name string `json:"name"`
}

type IssueFields struct {
	Components []Component `json:"components"`
}

type Issue struct {
	Fields IssueFields `json:"fields"`
}

type JiraResponse struct {
	Issues []Issue `json:"issues"`
}

type metricConfiguration struct {
	Name           string            `yaml:"name"`
	Help           string            `yaml:"help"`
	JQL            string            `yaml:"jql"`
	Interval       string            `yaml:"interval"`
	Labels         map[string]string `yaml:"labels"`
	ParsedInterval time.Duration
	GaugeVec       *prometheus.GaugeVec // Changed to GaugeVec for labels
}

type configuration struct {
	BaseURL     string                `yaml:"baseURL"`
	Login       string                `yaml:"login"`
	Password    string                `yaml:"password"`
	Metrics     []metricConfiguration `yaml:"metrics"`
	HTTPHeaders map[string]string     `yaml:"httpHeaders"`
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

func addHeaders(r *http.Request, headers map[string]string) {
	for k, v := range headers {
		r.Header.Set(k, v)
	}
}

func check(ctx context.Context, log *logrus.Logger, cfg *configuration, client *http.Client) {
	wg := sync.WaitGroup{}
	wg.Add(len(cfg.Metrics))
	for idx, m := range cfg.Metrics {
		go func(idx int, m metricConfiguration) {
			defer wg.Done()
			timer := time.NewTicker(m.ParsedInterval)
			defer timer.Stop()

		loop:
			for {
				func() { // Wrap in a closure to avoid goto jumping over declarations
					params := url.Values{}
					params.Set("jql", m.JQL)
					params.Set("maxResults", "100")
					params.Set("fields", "components")
					u := fmt.Sprintf("%s/rest/api/2/search?%s", cfg.BaseURL, params.Encode())

					log.Debugf("Checking %s", m.Name)
					r, err := http.NewRequest(http.MethodGet, u, nil)
					if err != nil {
						log.WithError(err).Errorf("Failed to create request for %s", u)
						return
					}
					addHeaders(r, cfg.HTTPHeaders)
					r.SetBasicAuth(cfg.Login, cfg.Password)

					resp, err := client.Do(r)
					if err != nil {
						log.WithError(err).Error("Request failed")
						return
					}
					defer resp.Body.Close()

					if resp.StatusCode != http.StatusOK {
						log.Errorf("Received status %d for %s", resp.StatusCode, u)
						return
					}

					var result JiraResponse
					if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
						log.WithError(err).Error("Failed to decode response")
						return
					}

					// Count components
					componentCounts := make(map[string]float64)
					for _, issue := range result.Issues {
						for _, comp := range issue.Fields.Components {
							componentCounts[comp.Name]++
						}
					}

					// Update metrics
					for comp, count := range componentCounts {
						cfg.Metrics[idx].GaugeVec.WithLabelValues(comp).Set(count)
					}
				}()

				select {
				case <-timer.C:
				case <-ctx.Done():
					break loop
				}
			}
			log.Infof("Stopping worker for %s", m.Name)
		}(idx, m)
	}
	wg.Wait()
}

func setupGauges(registry prometheus.Registerer, metrics []metricConfiguration) error {
	for i := 0; i < len(metrics); i++ {
		labelNames := make([]string, 0, len(metrics[i].Labels))
		for k := range metrics[i].Labels {
			labelNames = append(labelNames, k)
		}

		metrics[i].GaugeVec = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:        fmt.Sprintf("jira_%s", metrics[i].Name),
				Help:        metrics[i].Help,
			},
			labelNames,
		)

		if err := registry.Register(metrics[i].GaugeVec); err != nil {
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
	pflag.StringVar(&configFile, "config", "", "Path to configuration file")
	pflag.StringVar(&addr, "http-addr", "127.0.0.1:9300", "HTTP server address")
	pflag.BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	pflag.Parse()

	if verbose {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.InfoLevel)
	}

	if configFile == "" {
		log.Fatal("--config flag is required")
	}

	cfg, err := loadConfiguration(configFile)
	if err != nil {
		log.WithError(err).Fatal("Failed to load config")
	}

	if cfg.Password == "" {
		cfg.Password = os.Getenv("JIRA_PASSWORD")
		if cfg.Password == "" {
			log.Fatal("JIRA_PASSWORD environment variable not set")
		}
	}

	if err := setupGauges(prometheus.DefaultRegisterer, cfg.Metrics); err != nil {
		log.WithError(err).Fatal("Failed to setup gauges")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	httpServer := &http.Server{Addr: addr}
	httpClient := &http.Client{}

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-sigChan
		log.Info("Shutting down...")
		httpServer.Close()
		cancel()
	}()

	go func() {
		defer wg.Done()
		check(ctx, log, cfg, httpClient)
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	httpServer.Handler = mux

	log.Infof("Starting server on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.WithError(err).Fatal("HTTP server failed")
	}

	wg.Wait()
}
