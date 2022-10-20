package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	srv    *http.Server
	cfg    *Config
	gauges []prometheus.Gauge
}

var api TargetExporter

func (t *TargetExporter) StartMetrics() {
	go func() {
		log.Println("Loading targets")
		for nodeName, target := range t.cfg.Targets {
			log.Printf("target loaded: %s\n", nodeName)
			currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
				Name:        t.cfg.TargetMetricName,
				ConstLabels: map[string]string{"instance": nodeName},
			})

			currentGauge.Set(target)
			t.gauges = append(t.gauges, currentGauge)
		}

		log.Println("Starting metrics server")
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()
}

func (t *TargetExporter) StartApi() {
	go func() {
		log.Println("Starting API server")
		t.srv = setupRoutes()
		if err := t.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
}

func (t *TargetExporter) GetServer() *http.Server {
	return t.srv
}

func GetTargets(g *gin.Context) {
	time.Sleep(10 * time.Second)
	g.String(http.StatusOK, "Welcome Gin Server")
}

func setupRoutes() *http.Server {
	r := gin.Default()

	v1 := r.Group("/api/v1")
	{
		v1.GET("/targets", GetTargets)
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	return srv
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// TODO: Use gin to create the API
	// TODO: Implement graceful shutdown using context
	api.StartMetrics()
	api.StartApi()

	// Listen for the interrupt signal from the OS
	<-ctx.Done()

	// Restore default behavior on the interrupt signal and notify user of shutdown
	stop()
	log.Println("Shutting down gracefully, press Ctrl+C again to force")

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fmt.Print("ping")
	if err := api.GetServer().Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown: ", err)
	}

	log.Println("Server exiting")
}
