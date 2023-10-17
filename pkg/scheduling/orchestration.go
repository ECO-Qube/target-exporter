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
	JobName         string
	CpuTarget       int
	JobLength       int // in minutes
	CpuCount        int
	WorkloadType    string
	WorkingScenario map[string]float64
	StartDate       time.Time
}

type WorkloadSpawnOption func(*WorkloadSpawnOptions)

func JobName(name string) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.JobName = name
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

func StartDate(startDate time.Time) WorkloadSpawnOption {
	return func(options *WorkloadSpawnOptions) {
		options.StartDate = startDate
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
	targets           map[string]*Target
	pyzhmNodeMappings map[string]string
	logger            *zap.Logger
}

// NewOrchestrator initialized a new orchestrator for all scheduling strategies.
// By default, the schedulableStrategy is ON, the selfDrivingStrategy is OFF and the tawaStrategy is OFF.
func NewOrchestrator(kubeClient *Kubeclient, promClient *Promclient, pyzhmClient *PyzhmClient, logger *zap.Logger,
	targets map[string]*Target, schedulable map[string]*Schedulable, pyzhmNodeMappings map[string]string) *Orchestrator {
	schedulableStrategy := NewSchedulableStrategy(kubeClient, promClient, logger, targets, schedulable)
	schedulableStrategy.Start()
	// TODO: Check all suspended jobs having
	o := &Orchestrator{
		promClient:        promClient,
		kubeClient:        kubeClient,
		pyzhmClient:       pyzhmClient,
		selfDriving:       NewSelfDrivingStrategy(kubeClient, promClient, logger, targets),
		schedulable:       schedulableStrategy,
		tawa:              NewTawaStrategy(kubeClient, promClient, logger),
		targets:           targets,
		pyzhmNodeMappings: pyzhmNodeMappings,
		logger:            logger,
	}
	go o.CheckStartJobs()
	return o
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
		WithName(spawnOptions.JobName).
		WithCpuCount(spawnOptions.CpuCount).
		WithCpuLimit(cpuTarget).
		WithLength(MinutesToDuration(spawnOptions.JobLength)).
		// TODO: maybe needs more "intelligence"? for now, workload type -> hardware directly but in the future
		// it could be necessary to map workload type to hardware type depending on what type of workload we get
		// (e.g. AI workload -> GPU, etc.)
		WithWorkloadType(HardwareTarget(spawnOptions.WorkloadType)).
		WithStartDate(spawnOptions.StartDate)

	// Check if scenario present in HTTP request, if yes, don't read from Prometheus
	if o.IsTawaEnabled() {
		var currentEnergyConsumption map[string]float64
		if spawnOptions.WorkingScenario == nil || len(spawnOptions.WorkingScenario) == 0 {
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

		// Map node names according to yaml config since pyzhm uses different node names than Kubernetes node names
		actualNodeName := o.pyzhmNodeMappings[predictions.Assignments["job1"]]
		// Add node only if selection is on a node currently with actual_cpu < target_cpu, otherwise, continue
		diffNode, err := o.promClient.GetNodeCpuDiff(actualNodeName)
		if err != nil {
			o.logger.Error("failed to get node cpu diff", zap.Error(err))
			return err
		}
		if diffNode > 0 {
			jobBuilder.WithNodeSelector(actualNodeName)
		}
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

func (o *Orchestrator) CheckStartJobs() {
	for {
		suspendedJobs, err := o.kubeClient.GetSuspendedJobs()
		if err != nil {
			o.logger.Error("Error getting suspended jobs from API", zap.Error(err))
		}
		for _, suspendedJob := range suspendedJobs {
			jobStartDate, err := time.Parse(time.RFC3339, suspendedJob.Annotations[JobStartDateAnnotation])
			if err != nil {
				o.logger.Error("Error parsing date from JobSelectorLabel annotation", zap.Error(err))
			}
			if jobStartDate.Before(time.Now()) {
				err = o.kubeClient.StartSuspendedJob(suspendedJob.Name)
				if err != nil {
					o.logger.Error("Error starting suspended job", zap.Error(err))
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
}
