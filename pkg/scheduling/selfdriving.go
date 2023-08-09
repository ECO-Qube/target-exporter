package scheduling

import (
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"sync"
	"time"
)

const Start = "start"
const Stop = "stop"
const AdjustmentSlack = 0.05

type SelfDriving struct {
	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient
	targets    map[string]*Target
	logger     *zap.Logger

	startStop   chan string
	initialized bool
	mu          sync.Mutex
}

func NewSelfDriving(kubeClient *kubeclient.Kubeclient, promClient *promclient.Promclient, logger *zap.Logger, targets map[string]*Target) *SelfDriving {
	return &SelfDriving{
		kubeClient: kubeClient,
		promClient: promClient,
		logger:     logger,
		targets:    targets,

		startStop:   make(chan string),
		initialized: false,
	}
}

func (s *SelfDriving) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Println("Starting controller")

	if !s.initialized {
		s.initialized = true
		s.run()
	}
	s.startStop <- Start
}

func (s *SelfDriving) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Println("Stopping controller")

	s.startStop <- Stop
}

func (s *SelfDriving) run() {
	go func() {
		run := false
		for {
			select {
			case command, ok := <-s.startStop:
				if ok {
					if command == Start {
						run = true
					} else {
						run = false
					}
				} else {
					s.logger.Info("selfdriving startStop channel closed")
					break
				}
			default:
				s.logger.Debug("selfdriving startStop channel empty, continuing")
			}
			if run {
				err := s.Reconcile()
				if err != nil {
					s.logger.Error("error while reconciling", zap.Error(err))
				}
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

// See https://www.notion.so/e6e3f42774a54824acdacf2bfc1811e4?v=2555eddf50e54d8e87e367fd6feb8f43&p=e3be92a033fe417ebf9560f298c3297f&pm=c
func (s *SelfDriving) Reconcile() error {
	// Get current cpu diffs
	promClient := s.promClient
	kubeClient := s.kubeClient

	diffs, err := promClient.GetCurrentCpuDiff()
	if err != nil {
		return err
	}
	// For each node n that has diff > Target + (5%)
	for _, v := range diffs {
		if promclient.GetAvgInstantUsage(v.Data) > s.targets[v.NodeName].GetTarget()+AdjustmentSlack {
			s.logger.Info("Node above Target", zap.String("node", v.NodeName))
			// Get pods scheduled on n
			pods, err := kubeClient.GetPodsInNamespace()
			if err != nil {
				return err
			}
			podsInAboveTargetNode := make([]v1.Pod, 0)
			for _, pod := range pods.Items {
				if pod.Spec.NodeName == v.NodeName {
					podsInAboveTargetNode = append(podsInAboveTargetNode, pod)
				}
			}
			// delta := diff(n) / len(pods(n))
			delta := promclient.GetAvgInstantUsage(v.Data) / float64(len(podsInAboveTargetNode))
			newCpuLimit := s.targets[v.NodeName].GetTarget() - delta
			cpuCounts, err := promClient.GetCpuCounts()
			if err != nil {
				s.logger.Error("failed to get cpu counts", zap.Error(err))
				return err
			}
			deltaQuantity, err := kubeclient.PercentageToResourceQuantity(cpuCounts, newCpuLimit, v.NodeName)
			if err != nil {
				return err
			}
			// For each pod p
			for _, pod := range podsInAboveTargetNode {
				// patch(delta, p)
				err := kubeClient.PatchCpuLimit(deltaQuantity, pod.Name)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}
