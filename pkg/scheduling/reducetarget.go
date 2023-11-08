package scheduling

import (
	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
	"time"
)

type ReduceTargetsStrategy struct {
	*BaseConcurrentStrategy

	promClient *Promclient
	kubeClient *Kubeclient
	targets    map[string]*Target
	setpoints  []float64
	logger     *zap.Logger
}

func NewReduceTargetsStrategy(promClient *Promclient, kubeClient *Kubeclient, targets map[string]*Target, setpoints []float64, logger *zap.Logger) *ReduceTargetsStrategy {
	strategy := &ReduceTargetsStrategy{
		promClient: promClient,
		kubeClient: kubeClient,
		targets:    targets,
		setpoints:  setpoints,
		logger:     logger,
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("reduceTargets", strategy.Reconcile, logger.With(zap.String("strategy", "reduceTargets")))
	return strategy
}

func (r *ReduceTargetsStrategy) Reconcile() error {
	// If targets are below their target since X time, reduce to previous set point
	avgCpuUsage, err := r.promClient.GetAvgCpuUsages(5)
	if err != nil {
		r.logger.Error("failed to get avg cpu usages", zap.Error(err))
		return err
	}
	for nodeName, target := range r.targets {
		for _, avgUsage := range avgCpuUsage {
			if avgUsage.NodeName == nodeName && avgUsage.Data < target.GetTarget() {
				// Reduce target
				r.logger.Info("reducing target", zap.String("node", nodeName), zap.Float64("target", target.GetTarget()))
				target.Set(getLowerSetpoint(r.setpoints, target.GetTarget()))
			}
		}
	}
	time.Sleep(1 * time.Second)
	return nil
}

func (r *ReduceTargetsStrategy) IsAutomaticJobSpawnEnabled() bool {
	return r.isRunning
}

func (r *ReduceTargetsStrategy) Start() {
	r.BaseConcurrentStrategy.Start()
}

func (r *ReduceTargetsStrategy) Stop() {
	r.BaseConcurrentStrategy.Stop()
}

func getLowerSetpoint(setpoints []float64, currentTarget float64) float64 {
	for _, setpoint := range setpoints {
		if setpoint < currentTarget {
			return setpoint
		}
	}
	// If it's already the lowest, don't change it
	return currentTarget
}
