package scheduling

import (
	"fmt"
	"go.uber.org/zap"
)
import . "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
import . "git.helio.dev/eco-qube/target-exporter/pkg/serverswitch"

type ServerOnOffStrategy struct {
	*BaseConcurrentStrategy

	serverSwitches map[string]*IpmiServerSwitch
	promClient     *Promclient
	logger         *zap.Logger
}

func NewServerOnOffStrategy(serverSwitches map[string]*IpmiServerSwitch, promClient *Promclient, logger *zap.Logger) *ServerOnOffStrategy {
	strategy := &ServerOnOffStrategy{
		serverSwitches: serverSwitches,
		promClient:     promClient,
		logger:         logger,
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("serveronoff", strategy.Reconcile, logger.With(zap.String("strategy", "serveronoff")))
	return strategy
}

func (t *ServerOnOffStrategy) Reconcile() error {
	diffs, err := t.promClient.GetCurrentCpuDiff()
	if err != nil {
		t.logger.Error(fmt.Sprintf("error getting cpu diff: %s", err))
	}

	// Check if server is on
	for _, currentDiff := range diffs {
		for _, v := range diffs {
			serverOn, err := t.serverSwitches[currentDiff.NodeName].IsServerOn()
			if err != nil {
				t.logger.Error("error checking if server is on", zap.Error(err))
				return err
			}
			t.logger.Info("error checking if server is on", zap.Error(err), zap.Bool("isOn", serverOn), zap.String("server", v.NodeName))
		}
		//if isOn, err := t.serverSwitch.IsServerOn(); err != nil {
		//	t.logger.Info("error checking if server is on", zap.Error(err), zap.Bool("isOn", isOn), zap.String("server", t.serverSwitch.GetBmcEndpoint()))
		//}
	}

	return nil
}

func (t *ServerOnOffStrategy) Start() {
	t.BaseConcurrentStrategy.Start()
}

func (t *ServerOnOffStrategy) Stop() {
	t.BaseConcurrentStrategy.Stop()
}
