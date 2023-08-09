package scheduling

import (
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"strings"
	"sync"
	"time"
)

const Start = "start"
const Stop = "stop"
const AdjustmentSlack = 5

type SelfDriving struct {
	InFlight   bool
	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient
	targets    map[string]*Target

	logger      *zap.Logger
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
	s.InFlight = true

	fmt.Println("Starting controller")

	if !s.initialized {
		s.initialized = true
		s.run()
	}
	s.startStop <- Start
	s.InFlight = false
}

func (s *SelfDriving) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InFlight = true

	fmt.Println("Stopping controller")

	s.startStop <- Stop
	s.InFlight = false
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
			time.Sleep(10 * time.Second)
		}
	}()
}

// See https://www.notion.so/e6e3f42774a54824acdacf2bfc1811e4?v=2555eddf50e54d8e87e367fd6feb8f43&p=e3be92a033fe417ebf9560f298c3297f&pm=c
func (s *SelfDriving) Reconcile() error {
	// Get current cpu diffs
	promClient := s.promClient
	kubeClient := s.kubeClient

	s.logger.Debug("reconciling")

	diffs, err := promClient.GetCurrentCpuDiff()
	if err != nil {
		return err
	}
	// For each node n that has diff < -5
	for _, diff := range diffs {
		avgDiff := promclient.GetAvgInstantUsage(diff.Data)
		//target := s.targets[diff.NodeName].GetTarget()

		if avgDiff < -AdjustmentSlack {
			s.logger.Debug("Node above Target", zap.String("node", diff.NodeName),
				zap.Float64("target", s.targets[diff.NodeName].GetTarget()),
				zap.Float64("usage", -promclient.GetAvgInstantUsage(diff.Data)))
			// Get pods scheduled on n
			pods, err := kubeClient.GetPodsInNamespace()
			if err != nil {
				return err
			}
			podsInAboveTargetNode := make([]v1.Pod, 0)
			for _, pod := range pods.Items {
				if pod.Spec.NodeName == diff.NodeName {
					s.logger.Debug("Pod on Node detected, adjusting", zap.String("podName", pod.Name))
					podsInAboveTargetNode = append(podsInAboveTargetNode, pod)
				}
			}
			// delta := diff(n) / len(pods(n))
			absAvgDiff := math.Abs(avgDiff)
			delta := absAvgDiff / float64(len(podsInAboveTargetNode))
			cpuCounts, err := promClient.GetCpuCounts()
			if err != nil {
				s.logger.Error("failed to get cpu counts", zap.Error(err))
				return err
			}
			deltaQuantity, err := kubeclient.PercentageToResourceQuantity(cpuCounts, delta, diff.NodeName)
			if err != nil {
				return err
			}
			// For each pod p
			for _, pod := range podsInAboveTargetNode {
				// TODO: Fix this ugly hard-coded thing
				if strings.Contains(pod.Name, "telemetry-aware-scheduling") {
					continue
				}
				// patch(delta, p)
				s.logger.Debug("Patching pod", zap.String("podName", pod.Name),
					zap.String("node", diff.NodeName),
					zap.Float64("delta", delta),
					zap.Any("deltaQuantity", deltaQuantity))
				// TODO: What if there are more containers?
				// TODO: What if CPU limit result is negative?
				mystr := pod.Spec.Containers[0].Resources.Limits.Cpu().String()
				s.logger.Debug("CPU limit before sub", zap.String("cpuLimit", mystr))
				temp := pod.Spec.Containers[0].Resources.Limits.Cpu().Value() - deltaQuantity.MilliValue()
				newCpuLimitQuantity := resource.NewMilliQuantity(temp, resource.DecimalSI)
				mystr = pod.Spec.Containers[0].Resources.Limits.Cpu().String()
				s.logger.Debug("CPU limit after sub", zap.String("cpuLimit", newCpuLimitQuantity.String()))
				//newCpuLimitQuantity := pod.Spec.Containers[0].Resources.Limits.Cpu()
				err := kubeClient.PatchCpuLimit(*newCpuLimitQuantity, pod.Name)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}
