package scheduling

import (
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
)

type TawaStrategy struct {
	*BaseConcurrentStrategy
	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient
}

func NewTawaStrategy(kubeClient *kubeclient.Kubeclient, promClient *promclient.Promclient, logger *zap.Logger) *TawaStrategy {
	strategy := &TawaStrategy{
		kubeClient: kubeClient,
		promClient: promClient,
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("tawa", strategy.Reconcile, logger.With(zap.String("strategy", "tawa")))
	return strategy
}

func (s *TawaStrategy) Reconcile() error {
	// TODO: This is to be done for the "real" strategy, when we have a job queue. For now, we skip this.
	s.logger.Debug("not yet implemented")
	return nil
}
