package scheduling

import (
	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/pyzhm"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"time"
)

type WorkloadSpawnOptions struct {
	PodName         string
	CpuTarget       int
	JobLength       int
	CpuCount        int
	WorkloadType    string
	WorkingScenario map[string]float64
}

type WorkloadSpawnOption func(*WorkloadSpawnOptions)

func PodName(name string) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.PodName = name
	}
}

func CpuTarget(target int) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.CpuTarget = target
	}
}

func JobLength(length int) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.JobLength = length
	}
}

func CpuCount(count int) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.CpuCount = count
	}
}

func WorkloadType(workloadType string) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.WorkloadType = workloadType
	}
}

func WorkingScenario(scenario map[string]float64) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.WorkingScenario = scenario
	}
}

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
	pyzhmClient       *PyzhmClient
	selfDriving       *SelfDrivingStrategy
	schedulable       *SchedulableStrategy
	tawa              *TawaStrategy
	automaticJobSpawn *AutomaticJobSpawn
	targets           map[string]*Target
	logger            *zap.Logger
}

// NewOrchestrator initialized a new orchestrator for all scheduling strategies.
// By default, the schedulableStrategy is ON, the selfDrivingStrategy is OFF and the tawaStrategy is OFF.
func NewOrchestrator(kubeClient *Kubeclient, promClient *Promclient, pyzhmClient *PyzhmClient, logger *zap.Logger, targets map[string]*Target, schedulable map[string]*Schedulable) *Orchestrator {
	schedulableStrategy := NewSchedulableStrategy(kubeClient, promClient, logger, targets, schedulable)
	schedulableStrategy.Start()
	return &Orchestrator{
		promClient:        promClient,
		kubeClient:        kubeClient,
		pyzhmClient:       pyzhmClient,
		selfDriving:       NewSelfDrivingStrategy(kubeClient, promClient, logger, targets),
		schedulable:       schedulableStrategy,
		tawa:              NewTawaStrategy(kubeClient, promClient, logger),
		automaticJobSpawn: NewAutomaticJobSpawn(kubeClient, promClient, logger),
		targets:           targets,
		logger:            logger,
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

// AddWorkload adds a workload to the queue (for now, it spawns it directly).
func (o *Orchestrator) AddWorkload(options ...WorkloadSpawnOption) error {
	spawnOptions := &WorkloadSpawnOptions{}
	for _, setter := range options {
		setter(spawnOptions)
	}

	builder := NewConcreteStressJobBuilder()
	// TODO: get node name dynamically https://www.notion.so/helioag/Map-input-percentage-of-cpu-limits-to-range-of-CPUs-for-node-c6bd901a457243d5afece2ae0a9ac150?pvs=4
	var dummy string
	for k, _ := range o.targets {
		dummy = k
		break
	}
	cpuCounts, err := o.promClient.GetCpuCounts()
	if err != nil {
		o.logger.Error("failed to get cpu counts", zap.Error(err))
		return err
	}
	cpuTarget, err := PercentageToResourceQuantity(cpuCounts, float64(spawnOptions.CpuTarget), dummy)
	if err != nil {
		o.logger.Error("failed to convert cpu target to resource quantity", zap.Error(err))
		return err
	}

	jobBuilder := builder.
		WithCpuCount(spawnOptions.CpuCount).
		WithCpuLimit(cpuTarget).
		WithLength(time.Duration(spawnOptions.JobLength * int(time.Minute))).
		// TODO: maybe needs more "intelligence"? for now, workload type -> hardware directly but in the future
		// it could be necessary to map workload type to hardware type depending on what type of workload we get
		// (e.g. AI workload -> GPU, etc.)
		WithWorkloadType(HardwareTarget(spawnOptions.WorkloadType))

	// Check if scenario present in HTTP request, if yes, don't read from Prometheus
	if o.IsTawaEnabled() {
		var currentEnergyConsumption map[string]float64
		if spawnOptions.WorkingScenario == nil {
			currentEnergyConsumption, err = o.promClient.GetCurrentEnergyConsumption()
		} else {
			currentEnergyConsumption = spawnOptions.WorkingScenario
		}
		if err != nil {
			o.logger.Error("failed to get current energy consumption", zap.Error(err))
			return err
		}

		scenario := Scenario{
			Scenario:     make(map[string]float64),
			Requirements: make(map[string]float64),
		}
		// Map result to Scenario
		for k, v := range currentEnergyConsumption {
			scenario.Scenario[k] = v
		}

		coreCount, err := PercentageToResourceQuantity(cpuCounts, float64(spawnOptions.CpuTarget), dummy)
		if err != nil {
			o.logger.Error("failed to convert cpu target to resource quantity", zap.Error(err))
			return err
		}
		scenario.Requirements["job1"] = float64(coreCount.Value())

		predictions, err := o.pyzhmClient.Predict(scenario)
		if err != nil {
			o.logger.Error("failed to get predictions from pyzhm", zap.Error(err))
			return err
		}
		jobBuilder.WithNodeSelector(predictions.Assignments["job1"])
	}
	job, err := jobBuilder.Build()
	if err != nil {
		o.logger.Error("failed to build job", zap.Error(err))
		return err
	}

	// payload.CpuTarget, payload.CpuCount, time.Duration(payload.JobLength*int(time.Minute)), payload.HardwareTarget
	err = o.kubeClient.SpawnNewWorkload(job)
	if err != nil {
		o.logger.Error("failed to spawn new workload", zap.Error(err))
		return err
	}
	return nil
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
