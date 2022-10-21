package exporter

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
)

type Config struct {
	TargetMetricName string             `yaml:"targetMetricName"`
	Targets          map[string]float64 `yaml:"targets"`
}

type Target struct {
	target float64
	gauge  prometheus.Gauge
}

func (api *Target) Set(target float64) {
	api.gauge.Set(target)
	api.target = target
}

func (api *Target) GetTarget() float64 {
	return api.target
}

type TargetExporter struct {
	apiSrv     *http.Server
	metricsSrv *http.Server
	bootCfg    *Config
	targets    map[string]*Target
}

func NewTargetExporter(cfg *Config) *TargetExporter {
	return &TargetExporter{
		bootCfg: cfg,
		targets: make(map[string]*Target),
	}
}

func (t *TargetExporter) Targets() map[string]*Target {
	return t.targets
}

func (t *TargetExporter) StartMetrics() {
	log.Println("Loading targets")
	for nodeName, target := range t.bootCfg.Targets {
		log.Printf("target loaded: %s\n", nodeName)
		currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
			Name:        t.bootCfg.TargetMetricName,
			ConstLabels: map[string]string{"instance": nodeName},
		})
		currentGauge.Set(target)
		t.targets[nodeName] = &Target{target, currentGauge}
	}
	t.metricsSrv = &http.Server{
		Addr:    ":2112",
		Handler: promhttp.Handler(),
	}

	go func() {
		log.Println("Starting metrics server")
		if err := t.metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
}

func (t *TargetExporter) StartApi() {
	// Setup routes
	r := gin.Default()
	v1 := r.Group("/api/v1")
	{
		v1.GET("/targets", t.getTargetsResponse)
		v1.POST("/targets", t.postTargetsRequest)
	}
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}
	t.apiSrv = srv

	go func() {
		log.Println("Starting API server")
		if err := t.apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
}

func (t *TargetExporter) GetApiServer() *http.Server {
	return t.apiSrv
}

func (t *TargetExporter) GetMetricsServer() *http.Server {
	return t.metricsSrv
}

/************* API REQUESTS AND RESPONSE TYPES *************/

type TargetsResponse struct {
	Targets map[string]float64 `json:"targets"`
}

func (t *TargetExporter) getTargetsResponse(g *gin.Context) {
	payload := TargetsResponse{Targets: make(map[string]float64)}
	for node, target := range t.targets {
		payload.Targets[node] = target.GetTarget()
	}
	g.JSON(200, gin.H{
		"targets": payload,
	})
}

type TargetsRequest struct {
	Targets map[string]float64 `json:"targets"`
}

func (t *TargetExporter) postTargetsRequest(g *gin.Context) {
	payload := TargetsRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for node, target := range payload.Targets {
		if _, ok := t.Targets()[node]; ok {
			t.Targets()[node].Set(target)
		}
	}
	g.JSON(200, gin.H{
		"message": "success",
	})
}
