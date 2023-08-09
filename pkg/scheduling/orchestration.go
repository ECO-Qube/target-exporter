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
	selfDriving := NewSelfDriving(kubeClient, promClient, logger, targets)

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

func (o *Orchestrator) StartSelfDriving() {
	o.selfDriving.Start()
}

func (o *Orchestrator) StopSelfDriving() {
	o.selfDriving.Stop()
}
