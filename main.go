package main

import (
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

	/*	gaugeNode7qs6n = promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "cpu_diff_target",
			ConstLabels: map[string]string{"instance": "scheduling-dev-wkld-md-0-7qs6n"},
		})

		gaugeNodeQ9qgr = promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "cpu_diff_target",
			ConstLabels: map[string]string{"instance": "scheduling-dev-wkld-md-0-q9qgr"},
		})

		gaugeNodeV48np = promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "cpu_diff_target",
			ConstLabels: map[string]string{"instance": "scheduling-dev-wkld-md-0-v48np"},
		})
	*/
	/*	energyNode7qs6n = promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "energyConsumption",
			ConstLabels: map[string]string{"instance": "scheduling-dev-wkld-md-0-7qs6n"},
		})

		energyNodeQ9qgr = promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "energyConsumption",
			ConstLabels: map[string]string{"instance": "scheduling-dev-wkld-md-0-q9qgr"},
		})

		energyNodeV48np = promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "energyConsumption",
			ConstLabels: map[string]string{"instance": "scheduling-dev-wkld-md-0-v48np"},
		})*/
)

func init() {
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
	for nodeName, target := range cfg.Targets {
		fmt.Println(nodeName)
		currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "cpu_diff_target",
			ConstLabels: map[string]string{"instance": nodeName},
		})

		currentGauge.Set(target)
		gauges = append(gauges, currentGauge)
	}
	//gaugeNode7qs6n.Set(30)
	//gaugeNodeQ9qgr.Set(25)
	//gaugeNodeV48np.Set(40)

	//energyNode7qs6n.Set(300)
	//energyNodeQ9qgr.Set(250)
	//energyNodeV48np.Set(400)

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)
}
