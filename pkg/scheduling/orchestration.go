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
	promClient        *Promclient
	kubeClient        *Kubeclient
	logger            *zap.Logger
	selfDriving       *SelfDrivingStrategy
	schedulable       *SchedulableStrategy
	tawa              *TawaStrategy
	automaticJobSpawn *AutomaticJobSpawn
}

// NewOrchestrator initialized a new orchestrator for all scheduling strategies.
// By default, the schedulableStrategy is ON, the selfDrivingStrategy is OFF and the tawaStrategy is OFF.
func NewOrchestrator(kubeClient *Kubeclient, promClient *Promclient, logger *zap.Logger, targets map[string]*Target, schedulable map[string]*Schedulable) *Orchestrator {
	schedulableStrategy := NewSchedulableStrategy(kubeClient, promClient, logger, targets, schedulable)
	schedulableStrategy.Start()
	return &Orchestrator{
		promClient:        promClient,
		kubeClient:        kubeClient,
		logger:            logger,
		selfDriving:       NewSelfDrivingStrategy(kubeClient, promClient, logger, targets),
		schedulable:       schedulableStrategy,
		tawa:              NewTawaStrategy(kubeClient, promClient, logger),
		automaticJobSpawn: NewAutomaticJobSpawn(kubeClient, promClient, logger),
	}
}

func (o *Orchestrator) StartSelfDriving() {
	o.selfDriving.Start()
}

func (o *Orchestrator) StopSelfDriving() {
	o.selfDriving.Stop()
}

func (o *Orchestrator) IsSelfDrivingEnabled() bool {
	return o.selfDriving.IsRunning()
}

func (o *Orchestrator) StartSchedulable() {
	o.schedulable.Start()
}

func (o *Orchestrator) StopSchedulable() {
	o.schedulable.Stop()
}

func (o *Orchestrator) IsSchedulableEnabled() bool {
	return o.schedulable.IsRunning()
}

func (o *Orchestrator) StartTawa() {
	o.tawa.Start()
}

func (o *Orchestrator) StopTawa() {
	o.tawa.Stop()
}

func (o *Orchestrator) IsTawaEnabled() bool {
	return o.tawa.IsRunning()
}

func (o *Orchestrator) AddWorkload() {
	// TODO: Extract postWorkload login in here, check if TAWA strategy is enabled and schedule accordingly based on the
	// return value of pyZHM endpoint, create TAWA endpoint
	// TODO: Job queue, for now, don't keep a queue
}

func (o *Orchestrator) StartAutomaticJobSpawn() {
	o.automaticJobSpawn.Start()
}

func (o *Orchestrator) StopAutomaticJobSpawn() {
	o.automaticJobSpawn.Stop()
}

func (o *Orchestrator) IsAutomaticJobSpawnEnabled() bool {
	return o.automaticJobSpawn.IsRunning()
}
