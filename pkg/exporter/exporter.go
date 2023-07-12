package exporter

import (
	"context"
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/middlewares"
	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/resource"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
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

type Schedulable struct {
	schedulable bool
	gauge       prometheus.Gauge
}

func (api *Schedulable) Set(schedulable bool) {
	if schedulable {
		api.gauge.Set(1)
	} else {
		api.gauge.Set(0)
	}
	api.schedulable = schedulable
}

type TargetExporter struct {
	apiSrv       *http.Server
	metricsSrv   *http.Server
	promClient   *Promclient
	kubeClient   *Kubeclient
	logger       *zap.Logger
	bootCfg      Config
	targets      map[string]*Target
	schedulable  map[string]*Schedulable
	corsDisabled bool
}

func NewTargetExporter(cfg Config, kubeClient *Kubeclient, promClient *Promclient, promClientAddress string, logger *zap.Logger, corsEnabled bool) *TargetExporter {
	// Init Prometheus client
	client, err := api.NewClient(api.Config{
		// TODO: How to get address from v1.API embedded in Promclient instead of passing an additional parameter?
		Address: promClientAddress,
	})
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		os.Exit(1)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, warnings, err := v1api.Query(ctx, "up", time.Now(), v1.WithTimeout(5*time.Second))
	if err != nil {
		fmt.Printf("Error querying Prometheus: %v\n", err)
		os.Exit(1)
	}
	if len(warnings) > 0 {
		fmt.Printf("Warnings: %v\n", warnings)
	}
	fmt.Printf("Result:\n%v\n", result)

	return &TargetExporter{
		promClient:   promClient,
		kubeClient:   kubeClient,
		logger:       logger,
		bootCfg:      cfg,
		targets:      make(map[string]*Target), // basic cache for the targets, source of truth is in Prometheus TSDB
		schedulable:  make(map[string]*Schedulable),
		corsDisabled: corsEnabled,
	}
}

func (t *TargetExporter) Targets() map[string]*Target {
	return t.targets
}

func (t *TargetExporter) StartMetrics() {
	t.logger.Info("Loading targets")

	// Targets metrics
	for nodeName, target := range t.bootCfg.Targets {
		t.logger.Info(fmt.Sprintf("target loaded: %s\n", nodeName))
		currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
			Name:        t.bootCfg.TargetMetricName,
			ConstLabels: map[string]string{"instance": nodeName},
		})
		currentGauge.Set(target)
		t.targets[nodeName] = &Target{target, currentGauge}
	}

	// Export schedulable metrics
	for nodeName, _ := range t.bootCfg.Targets {
		t.logger.Info(fmt.Sprintf("target loaded: %s\n", nodeName))
		currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "schedulable",
			ConstLabels: map[string]string{"instance": nodeName},
		})
		currentGauge.Set(0)
		t.schedulable[nodeName] = &Schedulable{false, currentGauge}
	}

	go func() {
		// is there a node n where schedulable = 1?
		//    yes: is there a node n where diff > 0?
		//        yes: schedulable_n = 1; schedule()
		//        no: requeue()
		//    no: for n where schedulable_n = 1, is diff_n > 0?
		//        yes: schedule()
		//        no: schedulable_n = 0; pick another node where diff_n and set its schedulable to 1; schedule()
		for {
			diffs, err := t.promClient.GetCurrentCpuDiff()
			if err != nil {
				t.logger.Error(fmt.Sprintf("error getting cpu diff: %s", err))
			}
			if schedulableNode := t.findSchedulableNode(); schedulableNode == "" {
				t.logger.Debug("no schedulable node found")
				// All nodes are not schedulable, pick one with diff > 0
				for _, v := range diffs {
					if v.Data[0].Usage > 0 {
						t.logger.Info("found node with diff > 0, setting it to schedulable", zap.String("nodeName", v.NodeName))
						t.schedulable[v.NodeName].Set(true)
						break
					}
				}
			} else {
				t.logger.Debug("schedulable node found :tada:")
				// If current schedulable has exceeded target (diff is negative) change schedulable node, else continue
				for _, currentDiff := range diffs {
					if currentDiff.NodeName == schedulableNode && currentDiff.Data[0].Usage <= 0 {
						t.logger.Info("currently schedulable node has diff <= 0, picking another node", zap.String("nodeName", currentDiff.NodeName))
						t.schedulable[currentDiff.NodeName].Set(false)
						// Pick a node where diff > 0
						for _, newNodeDiff := range diffs {
							if newNodeDiff.Data[0].Usage > 0 {
								t.logger.Info("found node with diff > 0, setting it to schedulable", zap.String("nodeName", newNodeDiff.NodeName))
								t.schedulable[newNodeDiff.NodeName].Set(true)
								break
							}
						}
					}
				}
			}

			time.Sleep(1 * time.Second)
		}
	}()

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

	if t.corsDisabled {
		r.Use(middlewares.CorsDisabled)
	}
	v1 := r.Group("/api/v1")
	{
		v1.GET("/targets", t.getTargetsResponse)
		v1.POST("/targets", t.postTargetsRequest)

		v1.GET("/workloads", t.getWorkloads)
		v1.POST("/workloads", t.postWorkloads)
		v1.PATCH("/workload", t.patchWorkload)
		v1.DELETE("/workloads/completed", t.deleteWorkloadsCompleted)
		v1.DELETE("/workloads/pending/last", t.deleteWorkloadsPendingLast)

		v1.GET("/actualCpuUsageByRangeSeconds", t.getCpuUsageByRangeSeconds)
		v1.GET("/actualCpuDiff", t.getCurrentCpuDiff)
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

// if no message is returned to caller and 200 status,
// nothing was done but no error. If no error and something was deleted, "success" is returned
// In all other cases, an error is returned
func (t *TargetExporter) deleteWorkloadsCompleted(g *gin.Context) {
	done, err := t.kubeClient.ClearCompletedWorkloads()
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if done {
		g.JSON(http.StatusOK, gin.H{
			"message": "success",
		})
	}
}

// if no message is returned to caller and 200 status,
// nothing was done but no error. If no error and something was deleted, "success" is returned
// In all other cases, an error is returned
func (t *TargetExporter) deleteWorkloadsPendingLast(g *gin.Context) {
	done, err := t.kubeClient.DeletePendingWorkload()
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if done {
		g.JSON(http.StatusOK, gin.H{
			"message": "success",
		})
	}
}

type Workload struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	SubmissionDate string `json:"submissionDate"`
	NodeName       string `json:"nodeName"`
	CpuTarget      int    `json:"cpuTarget"`
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
		// TODO: Refactor
		target := pod.Spec.Containers[0].Resources.Limits.Cpu().AsDec().SetScale(1)
		targetFormatted, _ := strconv.Atoi(strings.Split(target.String(), ".")[0])
		if targetFormatted == 0 {
			targetFormatted = 100
		}
		workloads[i] = Workload{
			Name:           pod.Name,
			Status:         string(pod.Status.Phase),
			SubmissionDate: pod.CreationTimestamp.String(),
			NodeName:       pod.Spec.NodeName,
			CpuTarget:      targetFormatted,
		}
	}
	g.JSON(http.StatusOK, WorkloadsList{Workloads: workloads})
}

type WorkloadRequest struct {
	JobName      string       `json:"jobName,omitempty"`
	CpuTarget    int          `json:"cpuTarget"`
	JobLength    int          `json:"jobLength"`
	CpuCount     int          `json:"cpuCount"`
	WorkloadType WorkloadType `json:"workloadType"`
}

func (t *TargetExporter) postWorkloads(g *gin.Context) {
	// TODO: Note JobName can't be set by user yet
	payload := WorkloadRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	builder := NewConcreteStressJobBuilder()
	// TODO: Error handling
	// TODO: It probably makes sense to persist these jobs in a separate database...
	job, _ := builder.
		WithCpuCount(payload.CpuCount).
		WithCpuLimit(percentageToResourceQuantity(payload.CpuTarget)).
		WithLength(time.Duration(payload.JobLength * int(time.Minute))).
		WithWorkloadType(payload.WorkloadType).
		Build()

	// payload.CpuTarget, payload.CpuCount, time.Duration(payload.JobLength*int(time.Minute)), payload.WorkloadType
	err := t.kubeClient.SpawnNewWorkload(job)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}

func percentageToResourceQuantity(percentage int) resource.Quantity {
	return *resource.NewMilliQuantity(int64(percentage)*10, resource.DecimalSI)
}

func (t *TargetExporter) patchWorkload(g *gin.Context) {
	// TODO: Currently only supports patching of CPU limits
	payload := WorkloadRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if payload.JobName == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "jobName must be specified"})
		return
	}
	err := t.kubeClient.PatchCpuLimit(percentageToResourceQuantity(payload.CpuTarget), payload.JobName)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	t.logger.Info("workload patched", zap.String("workload", fmt.Sprintf("%v", payload)))
}

// GetCpuUsageByRangeSeconds returns a timeseries of the CPU usage of each node.
func (t *TargetExporter) getCpuUsageByRangeSeconds(g *gin.Context) {
	// Parse ISO date start and end from HTTP get request using Gin framework
	start, err := time.Parse(time.RFC3339, g.Query("start"))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	end, err := time.Parse(time.RFC3339, g.Query("end"))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cpuUsagesPerNode, err := t.promClient.GetCpuUsageByRangeSeconds(start, end)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	g.JSON(http.StatusOK, cpuUsagesPerNode)
}

func (t *TargetExporter) getCurrentCpuDiff(g *gin.Context) {
	cpuDiff, err := t.promClient.GetCurrentCpuDiff()
	// TODO: Better error handling
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, cpuDiff)
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

func (t *TargetExporter) findSchedulableNode() string {
	for k, v := range t.schedulable {
		if v.schedulable {
			return k
		}
	}
	return ""
}
