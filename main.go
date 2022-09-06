package main

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v2"
	"log"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	Targets map[string]float64
}

var (
	cfg    Config
	gauges []prometheus.Gauge
)

func init() {
	if _, err := os.Stat("./config.yaml"); errors.Is(err, os.ErrNotExist) {
		log.Fatalf("error: %v", err)
	}
	file, err := os.ReadFile("./config.yaml")
	if err != nil {
		return
	}
	err = yaml.Unmarshal(file, &cfg)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
}

func main() {
	fmt.Println("Loading targets:")
	for nodeName, target := range cfg.Targets {
		fmt.Println(nodeName)
		currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "cpu_diff_target",
			ConstLabels: map[string]string{"instance": nodeName},
		})

		currentGauge.Set(target)
		gauges = append(gauges, currentGauge)
	}

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)
}
