package exporter

import (
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/middlewares"
	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
	"time"

	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
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
	apiSrv      *http.Server
	metricsSrv  *http.Server
	kubeClient  *Kubeclient
	logger      *zap.Logger
	bootCfg     Config
	targets     map[string]*Target
	corsEnabled bool
}

func NewTargetExporter(cfg Config, kubeClient *Kubeclient, logger *zap.Logger, corsEnabled bool) *TargetExporter {
	return &TargetExporter{
		kubeClient:  kubeClient,
		logger:      logger,
		bootCfg:     cfg,
		targets:     make(map[string]*Target),
		corsEnabled: corsEnabled,
	}
}

func (t *TargetExporter) Targets() map[string]*Target {
	return t.targets
}

func (t *TargetExporter) StartMetrics() {
	t.logger.Info("Loading targets")
	for nodeName, target := range t.bootCfg.Targets {
		t.logger.Info(fmt.Sprintf("target loaded: %s\n", nodeName))
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
		t.logger.Info("Starting metrics server")
		if err := t.metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.logger.Fatal(fmt.Sprintf("listen: %s\n", err))
		}
	}()
}

func (t *TargetExporter) StartApi() {
	// Setup routes
	r := gin.Default()

	// Setup logger

	// Add a ginzap middleware, which:
	//   - Logs all requests, like a combined access and error log.
	//   - Logs to stdout.
	//   - RFC3339 with UTC time format.
	r.Use(ginzap.Ginzap(t.logger, time.RFC3339, true))

	// Logs all panic to error log
	//   - stack means whether output the stack info.
	r.Use(ginzap.RecoveryWithZap(t.logger, true))

	if t.corsEnabled {
		r.Use(middlewares.CorsEnabled)
	}
	v1 := r.Group("/api/v1")
	{
		v1.GET("/targets", t.getTargetsResponse)
		v1.POST("/targets", t.postTargetsRequest)

		v1.GET("/workloads", t.getWorkloads)
		v1.POST("/workloads", t.postWorkloads)
	}
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	t.apiSrv = srv

	go func() {
		t.logger.Info("Starting API server")
		if err := t.apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.logger.Fatal(fmt.Sprintf("listen: %s\n", err))
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
	g.JSON(http.StatusOK, payload)
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
	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}

type Workload struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	SubmissionDate string `json:"submissionDate"`
	NodeName       string `json:"nodeName"`
}

type WorkloadsList struct {
	Workloads []Workload `json:"workloads"`
}

// TODO: Make configurable with namespace or label selector
func (t *TargetExporter) getWorkloads(g *gin.Context) {
	pods, err := t.kubeClient.GetPodsInNamespace()
	if err != nil {
		// TODO: More granular error handling
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	workloads := make([]Workload, len(pods.Items))
	for i, pod := range pods.Items {
		workloads[i] = Workload{
			Name:           pod.Name,
			Status:         string(pod.Status.Phase),
			SubmissionDate: pod.CreationTimestamp.String(),
			NodeName:       pod.Spec.NodeName,
		}
	}
	g.JSON(http.StatusOK, WorkloadsList{Workloads: workloads})
}

func (t *TargetExporter) postWorkloads(g *gin.Context) {
	err := t.kubeClient.SpawnNewWorkload()
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}

/************* HELPER FUNCTIONS *************/

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
