package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
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

var isCorsEnabled = false
var logger *zap.Logger

func initLogger() {
	logger, _ = zap.NewProduction()
}

func initFlags() {
	// TODO: Make it proper with Cobra and Viper libraries
	flag.BoolVar(&isCorsEnabled, "cors-enabled", isCorsEnabled, "enable CORS for localhost:3000")

	flag.Parse()
}

func init() {
	initLogger()
	initFlags()

	if _, err := os.Stat("./config.yaml"); errors.Is(err, os.ErrNotExist) {
		logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
	}
	file, err := os.ReadFile("./config.yaml")
	if err != nil {
		logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
	}
	cfg := Config{}
	err = yaml.Unmarshal(file, &cfg)
	if err != nil {
		logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
	}
	api = NewTargetExporter(cfg, logger, isCorsEnabled)
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
	logger.Info("Shutting down gracefully, press Ctrl+C again to force")

	// The context is used to inform the server it has 30 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := api.GetApiServer().Shutdown(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("API server forced to shutdown: %s", err))
	}
	if err := api.GetMetricsServer().Shutdown(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("Metrics server forced to shutdown: %s", err))
	}
	logger.Info("Target Exporter exiting")
}
