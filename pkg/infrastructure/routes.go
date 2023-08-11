package infrastructure

import (
	"errors"
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/middlewares"
	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
	"time"
)

/************* API REQUESTS AND RESPONSE TYPES *************/

type TargetsResponse struct {
	Targets map[string]float64 `json:"targets"`
}

type TargetsRequest struct {
	Targets map[string]float64 `json:"targets"`
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

type WorkloadRequest struct {
	PodName      string                  `json:"podName,omitempty"`
	CpuTarget    int                     `json:"cpuTarget"`
	JobLength    int                     `json:"jobLength"`
	CpuCount     int                     `json:"cpuCount"`
	WorkloadType kubeclient.WorkloadType `json:"workloadType"`
}

type SelfDrivingRequest struct {
	Enabled bool `json:"enabled"`
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

		// TODO: Fix
		v1.GET("/self-driving", t.getSelfDriving)
		v1.PUT("/self-driving", t.putSelfDriving)
		v1.PUT("/test/self-driving", t.putTestSelfDriving)
	}
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	t.apiSrv = srv

	go func() {
		t.logger.Info("Starting API server")
		if err := t.apiSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.logger.Fatal(fmt.Sprintf("listen: %s\n", err))
		}
	}()
}

// TODO: Make configurable with namespace or label selector
func (t *TargetExporter) getWorkloads(g *gin.Context) {
	pods, err := t.kubeClient.GetPodsInNamespace()
	if err != nil {
		// TODO: More granular error handling
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cpuCounts, err := t.promClient.GetCpuCounts()
	if err != nil {
		t.logger.Error("failed to get cpu counts", zap.Error(err))
		return
	}

	workloads := make([]Workload, len(pods.Items))
	for i, pod := range pods.Items {
		target, err := kubeclient.ResourceQuantityToPercentage(cpuCounts, *pod.Spec.Containers[0].Resources.Limits.Cpu(), "")
		if err != nil {
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		workloads[i] = Workload{
			Name:           pod.Name,
			Status:         string(pod.Status.Phase),
			SubmissionDate: pod.CreationTimestamp.String(),
			NodeName:       pod.Spec.NodeName,
			CpuTarget:      int(target),
		}
	}
	g.JSON(http.StatusOK, WorkloadsList{Workloads: workloads})
}

func (t *TargetExporter) getTargetsResponse(g *gin.Context) {
	payload := TargetsResponse{Targets: make(map[string]float64)}
	for node, target := range t.targets {
		payload.Targets[node] = target.GetTarget()
	}
	g.JSON(http.StatusOK, payload)
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

func (t *TargetExporter) postWorkloads(g *gin.Context) {
	// TODO: Note PodName can't be set by user yet
	payload := WorkloadRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	builder := kubeclient.NewConcreteStressJobBuilder()
	// TODO: get node name dynamically https://www.notion.so/helioag/Map-input-percentage-of-cpu-limits-to-range-of-CPUs-for-node-c6bd901a457243d5afece2ae0a9ac150?pvs=4
	var dummy string
	for k, _ := range t.Targets() {
		dummy = k
		break
	}
	cpuCounts, err := t.promClient.GetCpuCounts()
	if err != nil {
		t.logger.Error("failed to get cpu counts", zap.Error(err))
		return
	}
	cpuTarget, err := kubeclient.PercentageToResourceQuantity(cpuCounts, float64(payload.CpuTarget), dummy)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	job, _ := builder.
		WithCpuCount(payload.CpuCount).
		WithCpuLimit(cpuTarget).
		WithLength(time.Duration(payload.JobLength * int(time.Minute))).
		WithWorkloadType(payload.WorkloadType).
		Build()

	// payload.CpuTarget, payload.CpuCount, time.Duration(payload.JobLength*int(time.Minute)), payload.WorkloadType
	err = t.kubeClient.SpawnNewWorkload(job)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}

func (t *TargetExporter) patchWorkload(g *gin.Context) {
	// TODO: Currently only supports patching of CPU limits
	payload := WorkloadRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if payload.PodName == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "podName must be specified"})
		return
	}
	nodeName, err := t.kubeClient.GetPodNodeName(payload.PodName)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if nodeName == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "cannot set CPU limit for a pod that is not in Running state"})
		return
	}
	cpuCounts, err := t.promClient.GetCpuCounts()
	if err != nil {
		t.logger.Error("failed to get cpu counts", zap.Error(err))
		return
	}
	cpuTarget, err := kubeclient.PercentageToResourceQuantity(cpuCounts, float64(payload.CpuTarget), nodeName)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	err = t.kubeClient.PatchCpuLimit(cpuTarget, payload.PodName)
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

func (t *TargetExporter) getSelfDriving(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{
		"enabled": t.o.IsSelfDrivingEnabled(),
	})
}

// TODO: Fix
func (t *TargetExporter) putSelfDriving(g *gin.Context) {
	payload := SelfDrivingRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Enabled {
		t.o.StartSelfDriving()
	} else {
		t.o.StopSelfDriving()
	}
	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}
