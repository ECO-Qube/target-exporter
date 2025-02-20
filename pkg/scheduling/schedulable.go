package scheduling

import (
	"fmt"
	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
)

type SchedulableStrategy struct {
	*BaseConcurrentStrategy

	targets     map[string]*Target
	schedulable map[string]*Schedulable

	kubeClient *Kubeclient
	promClient *Promclient
	logger     *zap.Logger
}

func NewSchedulableStrategy(kubeClient *Kubeclient, promClient *Promclient, logger *zap.Logger, targets map[string]*Target, schedulable map[string]*Schedulable) *SchedulableStrategy {
	strategy := &SchedulableStrategy{
		kubeClient:  kubeClient,
		promClient:  promClient,
		logger:      logger,
		targets:     targets,
		schedulable: schedulable,
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("schedulable", strategy.Reconcile, logger.With(zap.String("strategy", "schedulable")))
	return strategy
}

func (t *SchedulableStrategy) Reconcile() error {
	// is there a node n where Schedulable = 1?
	//	yes: is there a node n where diff > 0?
	//	    yes: schedulable_n = 1; schedule()
	//	    no: requeue()
	//	no: for n where schedulable_n = 1, is diff_n > 0?
	//	    yes: schedule()
	//	    no: schedulable_n = 0; pick another node where diff_n and set its Schedulable to 1; schedule()
	diffs, err := t.promClient.GetCurrentCpuDiff()
	if err != nil {
		t.logger.Error(fmt.Sprintf("error getting cpu diff: %s", err))
	}
	if schedulableNode := t.findSchedulableNode(); schedulableNode == "" {
		//t.logger.Debug("no schedulable node found")
		// All nodes are not Schedulable, pick one with diff > 0
		for _, v := range diffs {
			if v.Data[0].Usage > 0 {
				t.logger.Info("found node with diff > 0, setting it to Schedulable", zap.String("nodeName", v.NodeName))
				t.schedulable[v.NodeName].Set(true)
				break
			}
		}
	} else {
		//t.logger.Debug("schedulable node found :tada:")
		// If current Schedulable has exceeded Target (diff is negative) change Schedulable node, else continue
		for _, currentDiff := range diffs {
			if currentDiff.NodeName == schedulableNode && currentDiff.Data[0].Usage <= 0 {
				t.logger.Info("currently Schedulable node has diff <= 0, picking another node", zap.String("nodeName", currentDiff.NodeName))
				t.schedulable[currentDiff.NodeName].Set(false)
				// Pick a node where diff > 0
				for _, newNodeDiff := range diffs {
					if newNodeDiff.Data[0].Usage > 0 {
						t.logger.Info("found node with diff > 0, setting it to Schedulable", zap.String("nodeName", newNodeDiff.NodeName))
						t.schedulable[newNodeDiff.NodeName].Set(true)
						break
					}
				}
			}
		}
	}
	return nil
}

func (t *SchedulableStrategy) Stop() {
	t.BaseConcurrentStrategy.Stop()
	// Set all targets to 1
	for _, v := range t.schedulable {
		v.Set(true)
	}
}

// TODO: Remove duplicate in orchestration.go
func (t *SchedulableStrategy) findSchedulableNode() string {
	for k, v := range t.schedulable {
		if v.Schedulable {
			return k
		}
	}
	return ""
}
