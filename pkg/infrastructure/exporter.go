package infrastructure

import (
	"errors"
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/pyzhm"
	. "git.helio.dev/eco-qube/target-exporter/pkg/scheduling"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"net/http"
)

const (
	ErrNodeNonexistent = "specified node(s) does not exist"
)

type Config struct {
	TargetMetricName string             `yaml:"targetMetricName"`
	Targets          map[string]float64 `yaml:"targets"`
}

type TargetExporter struct {
	metricsSrv   *http.Server
	bootCfg      Config
	corsDisabled bool
	logger       *zap.Logger
	promClient   *promclient.Promclient
	kubeClient   *kubeclient.Kubeclient
	pyzhmClient  *pyzhm.PyzhmClient

	o           *Orchestrator
	apiSrv      *http.Server
	targets     map[string]*Target
	schedulable map[string]*Schedulable
}

func NewTargetExporter(promClient *promclient.Promclient, kubeClient *kubeclient.Kubeclient, metricsSrv *http.Server, bootCfg Config, corsDisabled bool, logger *zap.Logger) *TargetExporter {
	return &TargetExporter{
		promClient:   promClient,
		kubeClient:   kubeClient,
		pyzhmClient:  pyzhm.NewPyzhmClient(),
		metricsSrv:   metricsSrv,
		bootCfg:      bootCfg,
		corsDisabled: corsDisabled,
		logger:       logger,
		targets:      make(map[string]*Target), // basic cache for the targets, source of truth is in Prometheus TSDB
		schedulable:  make(map[string]*Schedulable),
	}
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
		t.targets[nodeName] = &Target{
			Target: target,
			Gauge:  currentGauge,
		}
	}

	// Export schedulable metrics
	for nodeName, _ := range t.bootCfg.Targets {
		t.logger.Info(fmt.Sprintf("gauges exported: %s\n", nodeName))
		currentGauge := promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "schedulable",
			ConstLabels: map[string]string{"instance": nodeName},
		})
		currentGauge.Set(0)
		t.schedulable[nodeName] = &Schedulable{Schedulable: false, Gauge: currentGauge}
	}

	// TODO: Remove
	// Set fake energy consumption
	fakeEnergyCons := map[string]float64{
		"L1":  163.47,
		"L3":  207.79,
		"L5":  144.51,
		"L7":  202.62,
		"L9":  187.44,
		"L11": 195.54,
		"L13": 208.63,
		"L15": 165.79,
		"L17": 179.72,
		"L19": 150.8,
		"L21": 193.27,
		"L23": 188.43,
		"R1":  73.1,
		"R3":  69.0,
		"R5":  134.96397857142858,
		"R7":  140.82715714285715,
		"R9":  134.96397857142858,
		"R11": 69.0,
		"R13": 69.0,
		"R15": 152.55351428571427,
		"R17": 69.0,
		"R19": 69.0,
		"R21": 69.0,
		"R23": 69.0,
	}

	for label, energyCons := range fakeEnergyCons {
		promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "fake_energy_consumption",
			ConstLabels: map[string]string{"node_label": label},
		}).Set(energyCons)
	}

	go func() {
		t.logger.Info("Starting metrics server")
		if err := t.metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.logger.Fatal(fmt.Sprintf("listen: %s\n", err))
		}
	}()
}

func (t *TargetExporter) Schedulable() map[string]*Schedulable {
	return t.schedulable
}

func (t *TargetExporter) Targets() map[string]*Target {
	return t.targets
}

func (t *TargetExporter) GetApiServer() *http.Server {
	return t.apiSrv
}

func (t *TargetExporter) GetMetricsServer() *http.Server {
	return t.metricsSrv
}

func (t *TargetExporter) SetOrchestrator(o *Orchestrator) {
	t.o = o
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
