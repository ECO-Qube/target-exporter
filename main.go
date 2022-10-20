package main

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	ErrLoadingConfigFile = "error loading config file"
)

type Config struct {
	TargetMetricName string             `yaml:"targetMetricName"`
	Targets          map[string]float64 `yaml:"targets"`
}

type TargetExporter struct {
	cfg    *Config
	gauges []prometheus.Gauge
}

var api TargetExporter

func (t *TargetExporter) StartMetrics() {
	go func() {
		log.Println("Loading targets")
		for nodeName, target := range api.cfg.Targets {
			log.Println(nodeName)
			currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
				Name:        api.cfg.TargetMetricName,
				ConstLabels: map[string]string{"instance": nodeName},
			})

			currentGauge.Set(target)
			api.gauges = append(api.gauges, currentGauge)
		}

		log.Println("Starting metrics server")
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()
}

func (t *TargetExporter) StartApi() {
	go func() {
		log.Println("Starting API server")
		r := setupRoutes()
		_ = r.Run(":8080")
	}()
}

func GetTargets(context *gin.Context) {
	g.JSON(http.StatusOK, "hello world")
}

func setupRoutes() *gin.Engine {
	r := gin.Default()

	v1 := r.Group("/api/v1")
	{
		v1.GET("/targets", GetTargets)
	}

	return r
}

func init() {
	if _, err := os.Stat("./config.yaml"); errors.Is(err, os.ErrNotExist) {
		log.Fatalf("%s: %v", ErrLoadingConfigFile, err)
	}
	file, err := os.ReadFile("./config.yaml")
	if err != nil {
		log.Fatalf("%s: %v", ErrLoadingConfigFile, err)
	}
	cfg := Config{}
	err = yaml.Unmarshal(file, &cfg)
	if err != nil {
		log.Fatalf("%s: %v", ErrLoadingConfigFile, err)
	}
	api = TargetExporter{
		cfg:    &cfg,
		gauges: []prometheus.Gauge{},
	}
}

func main() {
	// TODO: Use gin to create the API
	// TODO: Implement graceful shutdown using context
	api.StartMetrics()
	api.StartApi()
}
