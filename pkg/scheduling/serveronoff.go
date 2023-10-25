package scheduling

import (
	"go.uber.org/zap"
	"time"
)
import . "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
import . "git.helio.dev/eco-qube/target-exporter/pkg/serverswitch"

const AvgTimeUsageMins = 5 // in minutes
const MinAvgToTurnOff = 8  // in percentage

type ServerOnOffStrategy struct {
	*BaseConcurrentStrategy

	serverSwitches map[string]*IpmiServerSwitch
	promClient     *Promclient
	logger         *zap.Logger
	waitList       map[string]time.Time
}

func NewServerOnOffStrategy(serverSwitches map[string]*IpmiServerSwitch, promClient *Promclient, logger *zap.Logger) *ServerOnOffStrategy {
	strategy := &ServerOnOffStrategy{
		serverSwitches: serverSwitches,
		promClient:     promClient,
		logger:         logger,
		waitList:       make(map[string]time.Time),
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("serveronoff", strategy.Reconcile, logger.With(zap.String("strategy", "serveronoff")))
	return strategy
}

func (t *ServerOnOffStrategy) Reconcile() error {
	avgUsages, err := t.promClient.GetAvgCpuUsages(AvgTimeUsageMins)
	if err != nil {
		t.logger.Error("error getting avg cpu usages", zap.Error(err))
		return err
	}
	// Check if server is on
	for _, currentAvgUsage := range avgUsages {
		var isOn bool
		srvSwitch := t.serverSwitches[currentAvgUsage.NodeName]
		if isOn, err = t.serverSwitches[currentAvgUsage.NodeName].IsServerOn(); err != nil {
			t.logger.Error("error checking if server is on, retrying", zap.Error(err), zap.String("server", srvSwitch.GetBmcEndpoint()))
			retryServerConn(srvSwitch, t.logger)
		}
		t.logger.Info("checked if server is on", zap.Bool("isOn", isOn), zap.String("server", srvSwitch.GetBmcEndpoint()))
		//if isOn && (t.waitList[currentAvgUsage.NodeName].Before(time.Now()) || t.waitList[currentAvgUsage.NodeName].IsZero()) {
		//	if currentAvgUsage.Data < MinAvgToTurnOff {
		//		// Server is below min required avg usage to keep it switched on, turn off
		//		t.logger.Info("turning off server", zap.String("server", srvSwitch.GetBmcEndpoint()))
		//		if err = srvSwitch.PowerOff(); err != nil {
		//			t.logger.Error("error turning off server", zap.Error(err), zap.String("server", srvSwitch.GetBmcEndpoint()))
		//			// TODO: Can mess up things if the retry fails for a long time, so we assume a stable connection atm
		//			retryServerConn(srvSwitch, t.logger)
		//		} else {
		//			t.logger.Info("turned off server successfully", zap.String("server", srvSwitch.GetBmcEndpoint()))
		//			// We need to allow for the server to fully shutdown
		//			t.waitList[currentAvgUsage.NodeName] = time.Now().Add(1 * time.Minute)
		//		}
		//	}
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

func retryServerConn(srvSwitch *IpmiServerSwitch, logger *zap.Logger) {
	delay := 5 * time.Second
	for {
		err := srvSwitch.RetryConn()
		if err != nil {
			logger.Error("failed to retry connection", zap.Error(err), zap.String("server", srvSwitch.GetBmcEndpoint()))
			time.Sleep(delay)
		} else {
			break
		}
	}
}
