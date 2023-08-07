package scheduling

import (
	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type Target struct {
	Target float64
	Gauge  prometheus.Gauge
}

func (api *Target) Set(target float64) {
	api.Gauge.Set(target)
	api.Target = target
}

func (api *Target) GetTarget() float64 {
	return api.Target
}

type Schedulable struct {
	Schedulable bool
	Gauge       prometheus.Gauge
}

func (api *Schedulable) Set(schedulable bool) {
	if schedulable {
		api.Gauge.Set(1)
	} else {
		api.Gauge.Set(0)
	}
	api.Schedulable = schedulable
}

// Orchestrator is responsible for initializing and coordinating the scheduling / optimization strategies.
type Orchestrator struct {
	promClient  *Promclient
	kubeClient  *Kubeclient
	logger      *zap.Logger
	selfDriving *SelfDriving
	schedulable *SchedulableStrategy
}

func NewOrchestrator(kubeClient *Kubeclient, promClient *Promclient, logger *zap.Logger, targets map[string]*Target,
	schedulable map[string]*Schedulable) *Orchestrator {
	selfDriving := NewSelfDriving(kubeClient, promClient, logger)
	selfDriving.Start()

	schedulableStrategy := NewSchedulableStrategy(kubeClient, promClient, logger, targets, schedulable)
	schedulableStrategy.Start()
	return &Orchestrator{
		promClient:  promClient,
		kubeClient:  kubeClient,
		logger:      logger,
		selfDriving: selfDriving,
		schedulable: schedulableStrategy,
	}
}

func (o *Orchestrator) GetPromClient() *Promclient {
	return o.promClient
}

func (o *Orchestrator) GetKubeClient() *Kubeclient {
	return o.kubeClient
}

//func (t *Orchestrator) Targets() map[string]*Target {
//	return t.targets
//}
//
//func (t *Orchestrator) GetApiServer() *http.Server {
//	return t.apiSrv
//}
//
//func (t *Orchestrator) GetMetricsServer() *http.Server {
//	return t.metricsSrv
//}
//
///************* HELPER FUNCTIONS *************/
//
//func (t *Orchestrator) findSchedulableNode() string {
//	for k, v := range t.Schedulable {
//		if v.Schedulable {
//			return k
//		}
//	}
//	return ""
//}
