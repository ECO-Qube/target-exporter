package infrastructure

import (
	"errors"
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/middlewares"
	"git.helio.dev/eco-qube/target-exporter/pkg/pyzhm"
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
	Scenario     map[string]float64      `json:"scenario,omitempty"`
}

type enabled struct {
	Enabled bool `json:"enabled"`
}

type SelfDrivingRequest struct {
	enabled
}

type SchedulableRequest struct {
	enabled
}

type TawaRequest struct {
	enabled
}

type AutomaticJobSpawnRequest struct {
	enabled
}

func (t *TargetExporter) StartApi() {
	// Setup routes
	r := gin.New()

	// Setup logger

	// Add a ginzap middleware, which:
	//   - Logs all requests, like a combined access and error log.
	//   - Logs to stdout.
	//   - RFC3339 with UTC time format.
	//r.Use(ginzap.Ginzap(t.logger, time.RFC3339, true))

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

		v1.GET("/self-driving", t.getSelfDriving)
		v1.PUT("/self-driving", t.putSelfDriving)

		v1.GET("/tawa", t.getTawa)
		v1.PUT("/tawa", t.putTawa)

		v1.GET("/schedulable", t.getSchedulable)
		v1.PUT("/schedulable", t.putSchedulable)

		v1.GET("/automatic-job-spawn", t.getAutomaticJobSpawn)
		v1.PUT("/automatic-job-spawn", t.putAutomaticJobSpawn)
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

	jobBuilder := builder.
		WithCpuCount(payload.CpuCount).
		WithCpuLimit(cpuTarget).
		WithLength(time.Duration(payload.JobLength * int(time.Minute))).
		WithWorkloadType(payload.WorkloadType)

	// If TAWA strategy is on, add
	// TODO: Check if scenario present in HTTP request, if yes, don't read from Prometheus
	if t.o.IsTawaEnabled() {
		var currentEnergyConsumption map[string]float64
		if payload.Scenario == nil {
			currentEnergyConsumption, err = t.promClient.GetCurrentEnergyConsumption()
		} else {
			currentEnergyConsumption = payload.Scenario
		}
		if err != nil {
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		scenario := pyzhm.Scenario{
			Scenario:     make(map[string]float64),
			Requirements: make(map[string]float64),
		}
		// Map result to Scenario
		for k, v := range currentEnergyConsumption {
			scenario.Scenario[k] = v
		}

		// TODO: Get CPU count from Prometheus
		coreCount, err := kubeclient.PercentageToResourceQuantity(cpuCounts, float64(payload.CpuTarget), dummy)
		if err != nil {
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		scenario.Requirements["job1"] = float64(coreCount.Value())

		//scenario, err := t.pyzhmClient.GetTestScenario()
		if err != nil {
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		predictions, err := t.pyzhmClient.Predict(scenario)
		if err != nil {
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		jobBuilder.WithNodeSelector(predictions.Assignments["job1"])
	}
	job, err := jobBuilder.Build()
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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

func (t *TargetExporter) getSchedulable(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{
		"enabled": t.o.IsSchedulableEnabled(),
	})
}

func (t *TargetExporter) putSchedulable(g *gin.Context) {
	payload := SchedulableRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Enabled {
		t.o.StartSchedulable()
	} else {
		t.o.StopSchedulable()
	}
	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}

func (t *TargetExporter) getTawa(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{
		"enabled": t.o.IsTawaEnabled(),
	})
}

func (t *TargetExporter) putTawa(g *gin.Context) {
	payload := TawaRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Enabled {
		t.o.StartTawa()
	} else {
		t.o.StopTawa()
	}
	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}

func (t *TargetExporter) getAutomaticJobSpawn(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{
		"enabled": t.o.IsAutomaticJobSpawnEnabled(),
	})
}

func (t *TargetExporter) putAutomaticJobSpawn(g *gin.Context) {
	payload := AutomaticJobSpawnRequest{}
	if err := g.BindJSON(&payload); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Enabled {
		t.o.StartAutomaticJobSpawn()
	} else {
		t.o.StopAutomaticJobSpawn()
	}
	g.JSON(http.StatusOK, gin.H{
		"message": "success",
	})
}
