package scheduling

import (
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
const TimeSinceInsertionThreshold = 1 * time.Minute
const TimeSinceSchedulingThreshold = 1 * time.Minute

type SkipItem struct {
	PodName       string
	InsertionTime time.Time
	CpuLimit      resource.Quantity
}

type SkipList []SkipItem

type SelfDriving struct {
	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient
	targets    map[string]*Target

	logger      *zap.Logger
	startStop   chan string
	initialized bool
	mu          sync.Mutex
	skipForNow  SkipList
	IsRunning   bool
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

	s.logger.Debug("starting controller")
	if !s.initialized {
		s.initialized = true
		s.run()
	}
	s.startStop <- Start
}

func (s *SelfDriving) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("stopping controller")

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
						s.IsRunning = true
					} else {
						run = false
						s.IsRunning = false
					}
				} else {
					s.logger.Info("selfdriving startStop channel closed")
					break
				}
			default:
				s.logger.Debug("selfdriving startStop channel empty, continuing", zap.Bool("run", run))
			}
			if run {
				err := s.Reconcile()
				if err != nil {
					s.logger.Error("error while reconciling", zap.Error(err))
				}
			}
			time.Sleep(1 * time.Second)
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
		if avgDiff < -AdjustmentSlack {
			// Remove all items from skip where timeSinceInsertion > 2 minutes
			// Remove also completed pods
			pods, err := kubeClient.GetPodsInNamespace()
			if err != nil {
				return err
			}

			for i, skippedPod := range s.skipForNow {
				timeSinceInsertion := time.Since(skippedPod.InsertionTime)
				kubePod := getPodFromName(pods, skippedPod.PodName)
				if err != nil {
					s.logger.Error("failed to get pod from name", zap.Error(err))
					return err
				}
				if timeSinceInsertion > TimeSinceInsertionThreshold || isPodCompleted(kubePod) {
					s.logger.Debug("Removing item from skip list", zap.String("podName", s.skipForNow[i].PodName))
					s.skipForNow = removeIndex(s.skipForNow, i)
				}
			}
			s.logger.Debug("Node above Target", zap.String("node", diff.NodeName),
				zap.Float64("target", s.targets[diff.NodeName].GetTarget()),
				zap.Float64("usage", -promclient.GetAvgInstantUsage(diff.Data)))
			// Get pods scheduled on n
			podsInAboveTargetNode := make([]v1.Pod, 0)
			for _, pod := range pods.Items {
				if pod.Spec.NodeName == diff.NodeName {
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
				if strings.Contains(pod.Name, "telemetry-aware-scheduling") ||
					s.skipForNow.containsPod(pod.Name) {
					continue
				}

				timeSinceScheduling := getTimeSincePodScheduled(pod)
				if timeSinceScheduling < TimeSinceSchedulingThreshold {
					s.logger.Debug("Pod has been scheduled for less than 2 minutes, skipping",
						zap.String("podName", pod.Name),
						zap.Duration("timeSinceScheduling", timeSinceScheduling))
					continue
				}

				newCpuLimit := pod.Spec.Containers[0].Resources.Limits.Cpu().DeepCopy()
				newCpuLimit.Sub(deltaQuantity)

				// patch(delta, p)
				// TODO: What if there are more containers?
				// TODO: What if CPU limit result is negative?
				s.skipForNow = append(s.skipForNow, SkipItem{
					PodName:       pod.Name,
					InsertionTime: time.Now(),
					CpuLimit:      newCpuLimit,
				})
				err := kubeClient.PatchCpuLimit(newCpuLimit, pod.Name)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func isPodCompleted(pod *v1.Pod) bool {
	if pod == nil {
		return false
	}
	return pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}

func getPodFromName(podList *v1.PodList, name string) *v1.Pod {
	for _, pod := range podList.Items {
		if pod.Name == name {
			return &pod
		}
	}
	return nil
}

func getTimeSincePodScheduled(pod v1.Pod) time.Duration {
	for _, v := range pod.Status.Conditions {
		if v.Type == v1.PodScheduled {
			return time.Since(v.LastTransitionTime.Time)
		}
	}
	return time.Duration(0)
}

func removeIndex(s SkipList, index int) SkipList {
	ret := make(SkipList, 0)
	ret = append(ret, s[:index]...)
	return append(ret, s[index+1:]...)
}

func (s SkipList) containsPod(podName string) bool {
	for _, v := range s {
		if v.PodName == podName {
			return true
		}
	}
	return false
}

func (s SkipList) isInsertedMoreThan(podName string, d time.Duration) bool {
	for _, v := range s {
		if v.PodName == podName {
			if time.Since(v.InsertionTime) > d {
				return true
			}
		}
	}
	return false
}
