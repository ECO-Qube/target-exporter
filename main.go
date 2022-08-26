package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

var (
	gaugeNode7qs6n = promauto.NewGauge(prometheus.GaugeOpts{
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
)

func exportFakeEnergyTargets() {
	// TODO: Export fake targets using fake values, e.g. P = 5kW + (cpu% * 1kW)
}

func main() {
	gaugeNode7qs6n.Set(30)
	gaugeNodeQ9qgr.Set(25)
	gaugeNodeV48np.Set(40)

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)
}
