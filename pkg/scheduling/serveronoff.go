package scheduling

import (
	"fmt"
	"go.uber.org/zap"
	"time"
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
		var isOn bool
		if isOn, err = t.serverSwitches[currentDiff.NodeName].IsServerOn(); err != nil {
			t.logger.Error("error checking if server is on, retrying", zap.Error(err), zap.String("server", t.serverSwitches[currentDiff.NodeName].GetBmcEndpoint()))
			retryServerConn(t.serverSwitches[currentDiff.NodeName], t.logger)
		}
		t.logger.Info("checked if server is on", zap.Bool("isOn", isOn), zap.String("server", t.serverSwitches[currentDiff.NodeName].GetBmcEndpoint()))
	}

	return nil
}

func (t *ServerOnOffStrategy) Start() {
	t.BaseConcurrentStrategy.Start()
}

func (t *ServerOnOffStrategy) Stop() {
	t.BaseConcurrentStrategy.Stop()
}

func retryServerConn(srvSwitch *IpmiServerSwitch, logger *zap.Logger) {
	retries := 3
	delay := 5 * time.Second
	for i := 0; i < retries; i++ {
		err := srvSwitch.RetryConn()
		if err != nil {
			logger.Error("failed to retry connection", zap.Error(err), zap.String("server", srvSwitch.GetBmcEndpoint()))
			time.Sleep(delay)
		} else {
			break
		}
	}
}
