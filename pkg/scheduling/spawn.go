package scheduling

import (
	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
)

type AutomaticJobSpawn struct {
	*BaseConcurrentStrategy

	kubeClient *Kubeclient
	promClient *Promclient
	logger     *zap.Logger
}

func NewAutomaticJobSpawn(kubeClient *Kubeclient, promClient *Promclient, logger *zap.Logger) *AutomaticJobSpawn {
	strategy := &AutomaticJobSpawn{
		kubeClient: kubeClient,
		promClient: promClient,
		logger:     logger,
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("automaticJobSpawn", strategy.Reconcile, logger.With(zap.String("strategy", "automaticJobSpawn")))
	return strategy
}

func (t *AutomaticJobSpawn) Reconcile() error {

	return nil
}

func (t *AutomaticJobSpawn) Stop() {
	t.BaseConcurrentStrategy.Stop()
}
