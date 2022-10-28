package main

import (
	"context"
	"errors"
	"flag"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	. "git.helio.dev/eco-qube/target-exporter/pkg/exporter"
)

const (
	ErrLoadingConfigFile = "error loading config file"
)

var api *TargetExporter

var isDebugModeEnabled = false

func initFlags() {
	// TODO: Make it proper with Cobra and Viper libraries
	flag.BoolVar(&isDebugModeEnabled, "debug", isDebugModeEnabled, "enable CORS for localhost:3000")
}

func init() {
	initFlags()

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
	api = NewTargetExporter(cfg, isDebugModeEnabled)
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

	// The context is used to inform the server it has 30 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := api.GetApiServer().Shutdown(ctx); err != nil {
		log.Fatal("API server forced to shutdown: ", err)
	}
	if err := api.GetMetricsServer().Shutdown(ctx); err != nil {
		log.Fatal("Metrics server forced to shutdown: ", err)
	}
	log.Println("Target Exporter exiting")
}
