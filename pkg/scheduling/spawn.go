package scheduling

import (
	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
	"strconv"
	"time"
)

const CpuPercentageRoom = 20
const SpawnJobCpuPercentage = 10

// MaxBurstPerNode When spawning a burst of jobs, how many jobs per node at most (could be less, if the node is already at capacity)
const MaxBurstPerNode = 4

// BurstResetTimeout How long to wait before resetting the burst counter, by default set to 5 mins like the default job length
const BurstResetTimeout = 1 * time.Minute
const ReconciliationDelay = 20 * time.Second

type AutomaticJobSpawn struct {
	*BaseConcurrentStrategy

	o          *Orchestrator
	kubeClient *Kubeclient
	promClient *Promclient
	logger     *zap.Logger

	resetTime  time.Time
	spawnCount int
}

func NewAutomaticJobSpawn(orchestrator *Orchestrator, kubeClient *Kubeclient, promClient *Promclient, logger *zap.Logger) *AutomaticJobSpawn {
	strategy := &AutomaticJobSpawn{
		o:          orchestrator,
		kubeClient: kubeClient,
		promClient: promClient,
		logger:     logger,

		resetTime:  time.Now(),
		spawnCount: 0,
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("automaticJobSpawn", strategy.Reconcile, logger.With(zap.String("strategy", "automaticJobSpawn")))
	return strategy
}

func (t *AutomaticJobSpawn) Reconcile() error {
	// is there room for a new job?
	//	yes: spawn a new job
	//	no: requeue()
	t.logger.Debug("reconciling automatic job spawn")
	diffs, err := t.promClient.GetCurrentCpuDiff()
	if err != nil {
		t.logger.Error("error getting cpu diff", zap.Error(err))
		return err
	}
	for _, v := range diffs {
		if shouldReset(t.resetTime, t.spawnCount) {
			t.spawnCount = 0
			t.resetTime = time.Now().Add(BurstResetTimeout)
		}
		if shouldSpawn(v.Data[len(v.Data)-1].Usage, t.spawnCount, t.resetTime) {
			t.logger.Debug("spawning", zap.Float64("diff", v.Data[len(v.Data)-1].Usage),
				zap.String("nodeName", v.NodeName),
				zap.String("resetTime", t.resetTime.String()),
				zap.Int("spawnCount", t.spawnCount),
			)
			t.logger.Info("found node with diff >= "+strconv.Itoa(CpuPercentageRoom)+", spawning a new job", zap.String("nodeName", v.NodeName))
			cpuCounts, err := t.promClient.GetCpuCounts()
			cpuCount := cpuCounts[v.NodeName]
			if err != nil {
				t.logger.Error("error getting cpu count", zap.Error(err))
				return err
			}
			opts := []WorkloadSpawnOption{
				CpuTarget(SpawnJobCpuPercentage),
				JobLength(5),
				CpuCount(cpuCount),
				WorkingScenario(map[string]float64{}),
			}

			err = t.o.AddWorkload(opts...)
			if err != nil {
				t.logger.Error("error adding workload", zap.Error(err))
				return err
			}
			t.spawnCount++
			break
		}
	}
	time.Sleep(ReconciliationDelay)

	return nil
}

func (t *AutomaticJobSpawn) IsAutomaticJobSpawnEnabled() bool {
	return t.isRunning
}

func (t *AutomaticJobSpawn) Start() {
	t.BaseConcurrentStrategy.Start()
}

func (t *AutomaticJobSpawn) Stop() {
	t.BaseConcurrentStrategy.Stop()
}

func shouldReset(resetTime time.Time, spawnCount int) bool {
	return resetTime.Before(time.Now()) && spawnCount >= MaxBurstPerNode
}

func shouldSpawn(diff float64, spawnCount int, resetTime time.Time) bool {
	return diff >= 0 && spawnCount < MaxBurstPerNode && resetTime.Before(time.Now())
}
