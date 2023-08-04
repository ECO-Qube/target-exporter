package exporter

import (
	"context"
	"errors"
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/selfdriving"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/resource"
	"net/http"
	"os"
	"time"

	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/selfdriving"
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
	selfDriving  *SelfDriving
	logger       *zap.Logger
	bootCfg      Config
	targets      map[string]*Target
	schedulable  map[string]*Schedulable
	corsDisabled bool
}

func NewTargetExporter(cfg Config, kubeClient *Kubeclient, promClient *Promclient, promClientAddress string, logger *zap.Logger, corsEnabled bool, selfDriving *selfdriving.SelfDriving) *TargetExporter {
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
		selfDriving:  selfDriving,
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
		if err := t.metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	PodName      string       `json:"podName,omitempty"`
	CpuTarget    int          `json:"cpuTarget"`
	JobLength    int          `json:"jobLength"`
	CpuCount     int          `json:"cpuCount"`
	WorkloadType WorkloadType `json:"workloadType"`
}

type SelfDrivingRequest struct {
	Enabled bool `json:"enabled"`
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

// percentageToResourceQuantity converts a percentage to a resource.Quantity taking into account
// the number of CPU cores on the machine
func (t *TargetExporter) percentageToResourceQuantity(percentage float64, nodeName string) (resource.Quantity, error) {
	// Get schedulable node
	cpuCounts, err := t.promClient.GetCpuCounts()
	if err != nil {
		t.logger.Error("failed to get cpu counts", zap.Error(err))
		return resource.Quantity{}, err
	}
	cpuCount := cpuCounts[nodeName]
	// Map percentage to range 0-num_cpus
	percentage = (percentage / 100) * float64(cpuCount)
	return *resource.NewMilliQuantity(int64(percentage*1000), "DecimalSI"), nil
}

func (t *TargetExporter) resourceQuantityToPercentage(quantity resource.Quantity, nodeName string) (float64, error) {
	// Get schedulable node
	cpuCounts, err := t.promClient.GetCpuCounts()
	if err != nil {
		t.logger.Error("failed to get cpu counts", zap.Error(err))
		return 0, err
	}
	// TODO: Eventually get rid of this (when CPU counts will be heterogeneous)
	if nodeName == "" {
		for k, _ := range cpuCounts {
			nodeName = k
			break
		}
	}
	cpuCount := cpuCounts[nodeName]
	// Map quantity to range 0-num_cpus
	percentage := (float64(quantity.MilliValue()) / 1000) / float64(cpuCount)
	return percentage * 100, nil
}
