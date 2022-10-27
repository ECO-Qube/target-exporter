package exporter

import (
	"git.helio.dev/eco-qube/target-exporter/pkg/middlewares"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
)

const (
	ErrNodeNonexistent = "specified node(s) does not exist"
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
	bootCfg    Config
	targets    map[string]*Target
}

func NewTargetExporter(cfg Config) *TargetExporter {
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
	// TODO: Use CorsEnabledServer only during development; add a CLI flag to set development mode.
	v1 := r.Group("/api/v1", middlewares.CorsEnabledMiddleware)
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
	// TODO: Remove debug code
	// time.Sleep(500 * time.Millisecond)
	// g.JSON(404, "error")
	payload := TargetsResponse{Targets: make(map[string]float64)}
	for node, target := range t.targets {
		payload.Targets[node] = target.GetTarget()
	}
	g.JSON(200, payload)
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
	// For now, any missing node in the payload will fail the whole request. Might make this more lenient in the future.
	if missingNodes := checkMissingNodes(t.targets, payload.Targets); len(missingNodes) > 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": ErrNodeNonexistent, "nodes": missingNodes})
		return
	}
	for node, target := range payload.Targets {
		t.Targets()[node].Set(target)
	}
	g.JSON(200, gin.H{
		"message": "success",
	})
}

// Helper function to find missing nodes from one map where key is node name, and a map of node names to *Target.
// Returns nil if no missing nodes were found.
func checkMissingNodes(targets map[string]*Target, targetsToCheck map[string]float64) []string {
	missing := make([]string, 0)
	for node, _ := range targetsToCheck {
		if _, exists := targets[node]; !exists {
			missing = append(missing, node)
		}
	}
	return missing
}
