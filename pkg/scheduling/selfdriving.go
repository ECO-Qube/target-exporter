package scheduling

import (
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"time"
)

const AdjustmentSlack = 2
const TimeSinceInsertionThreshold = 1 * time.Minute
const TimeSinceSchedulingThreshold = 2 * time.Minute

type SkipItem struct {
	PodName       string
	InsertionTime time.Time
	CpuLimit      resource.Quantity
}

type SkipList []SkipItem

type SelfDrivingStrategy struct {
	*BaseConcurrentStrategy

	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient
	targets    map[string]*Target
	skipForNow SkipList
}

func NewSelfDrivingStrategy(kubeClient *kubeclient.Kubeclient, promClient *promclient.Promclient, logger *zap.Logger, targets map[string]*Target) *SelfDrivingStrategy {
	strategy := &SelfDrivingStrategy{
		kubeClient: kubeClient,
		promClient: promClient,
		targets:    targets,
		skipForNow: make(SkipList, 0),
	}
	strategy.BaseConcurrentStrategy = NewBaseConcurrentStrategy("selfDriving", strategy.Reconcile, logger.With(zap.String("strategy", "selfDriving")))
	return strategy
}

// See https://www.notion.so/e6e3f42774a54824acdacf2bfc1811e4?v=2555eddf50e54d8e87e367fd6feb8f43&p=e3be92a033fe417ebf9560f298c3297f&pm=c
func (s *SelfDrivingStrategy) Reconcile() error {
	promClient := s.promClient
	kubeClient := s.kubeClient
	cpuCounts, err := promClient.GetCpuCounts()

	diffs, err := promClient.GetCurrentCpuDiff()
	if err != nil {
		return err
	}

	for _, nodeDiff := range diffs {
		avgDiff := promclient.GetAvgInstantUsage(nodeDiff.Data)
		// If node is within thresholds of being OK, go to next node if present
		if !isNodeInViolation(avgDiff) {
			continue
		}
		pods, err := kubeClient.GetPodsInNamespaceByNode(nodeDiff.NodeName)
		if err != nil {
			return err
		}
		deltas, err := getViolatingPodsDelta(cpuCounts, nodeDiff, pods)
		if err != nil {
			s.logger.Error("failed to get violating pods delta", zap.Error(err))
			return err
		}
		for k, v := range deltas {
			if v != 0 {
				// patch
				perc, err := kubeclient.PercentageToResourceQuantity(cpuCounts, v, k)
				if err != nil {
					s.logger.Error("failed to convert resource quantity to percentage", zap.Error(err))
					return err
				}
				err = kubeClient.PatchCpuLimit(perc, k)
				if err != nil {
					s.logger.Error("failed to patch cpu limit", zap.Error(err))
					return err
				}
			}
		}
	}
	return nil
}

func isNodeInViolation(avgDiff float64) bool {
	return isNodeAboveTarget(avgDiff) || isNodeBelowTarget(avgDiff)
}

//func (s *SelfDrivingStrategy) Reconcile() error {
//	// Get current cpu diffs
//	promClient := s.promClient
//	kubeClient := s.kubeClient
//
//	diffs, err := promClient.GetCurrentCpuDiff()
//	if err != nil {
//		return err
//	}
//	// For each node N
//	for _, diff := range diffs {
//		`avgDiff := promclient.GetAvgInstantUsage(diff.Data)`
//		s.logger.Info("avgDiff", zap.Float64("avgDiff", avgDiff))
//		if isNodeAboveTarget(avgDiff) || isNodeBelowTarget(avgDiff) {
//			// Remove all items from skip where timeSSi > 1m OR where the corresponding pod is completed
//			pods, err := kubeClient.GetPodsInNamespace()
//			if err != nil {
//				return err
//			}
//			for i, skippedPod := range s.skipForNow {
//				timeSinceInsertion := time.Since(skippedPod.InsertionTime)
//				kubePod := getPodFromName(pods, skippedPod.PodName)
//				if err != nil {
//					s.logger.Error("failed to get pod from name", zap.Error(err))
//					return err
//				}
//				if timeSinceInsertion > TimeSinceInsertionThreshold || isPodCompleted(kubePod) {
//					s.logger.Debug("removing item from skip list", zap.String("podName", s.skipForNow[i].PodName))
//					s.skipForNow = removeIndex(s.skipForNow, i)
//				}
//			}
//			s.logger.Debug("Node violating Target", zap.String("node", diff.NodeName),
//				zap.Float64("target", s.targets[diff.NodeName].GetTarget()),
//				zap.Float64("usage", -promclient.GetAvgInstantUsage(diff.Data)))
//			// Get pods P in N NOT in skip and that are running
//			podsInViolatingNode := make([]v1.Pod, 0)
//			for _, pod := range pods.Items {
//				if pod.Spec.NodeName == diff.NodeName && pod.Status.Phase == v1.PodRunning {
//					podsInViolatingNode = append(podsInViolatingNode, pod)
//				}
//			}
//			// delta := diff(n) / len(pods(n))
//			absAvgDiff := math.Abs(avgDiff)
//			delta := absAvgDiff / float64(len(podsInViolatingNode))
//			cpuCounts, err := promClient.GetCpuCounts()
//			if err != nil {
//				s.logger.Error("failed to get cpu counts", zap.Error(err))
//				return err
//			}
//			deltaQuantity, err := kubeclient.PercentageToResourceQuantity(cpuCounts, delta, diff.NodeName)
//			if err != nil {
//				return err
//			}
//			// For each pod p
//			for _, pod := range podsInViolatingNode {
//				// TODO: Fix this ugly hard-coded thing
//				if strings.Contains(pod.Name, "telemetry-aware-scheduling") ||
//					s.skipForNow.containsPod(pod.Name) {
//					continue
//				}
//
//				timeSinceScheduling := getTimeSincePodScheduled(pod)
//				if timeSinceScheduling < TimeSinceSchedulingThreshold {
//					s.logger.Debug("Pod has been scheduled for less than 2 minutes, skipping",
//						zap.String("podName", pod.Name),
//						zap.Duration("timeSinceScheduling", timeSinceScheduling))
//					continue
//				}
//
//				newCpuLimit := pod.Spec.Containers[0].Resources.Limits.Cpu().DeepCopy()
//				if isNodeAboveTarget(avgDiff) {
//					newCpuLimit.Sub(deltaQuantity)
//				} else if isNodeBelowTarget(avgDiff) {
//					newCpuLimit.Add(deltaQuantity)
//				}
//
//				// patch(delta, p)
//				// TODO: What if there are more containers?
//				// TODO: What if CPU limit result is negative?
//				s.skipForNow = append(s.skipForNow, SkipItem{
//					PodName:       pod.Name,
//					InsertionTime: time.Now(),
//					CpuLimit:      newCpuLimit,
//				})
//				err := kubeClient.PatchCpuLimit(newCpuLimit, pod.Name)
//				if err != nil {
//					return err
//				}
//			}
//		}
//	}
//
//	return nil
//}

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

func isNodeAboveTarget(avgDiff float64) bool {
	return avgDiff < -AdjustmentSlack
}

func isNodeBelowTarget(avgDiff float64) bool {
	return avgDiff > AdjustmentSlack
}

// getViolatingPodsDelta returns a map of pod names to cpu deltas that needs updating by a certain delta.
// Values can also be 0. Negative values mean that the pod needs to be throttled, positive values mean that the pod CPU limit can be increased.
func getViolatingPodsDelta(cpuCounts map[string]int, diffs promclient.NodeCpuUsage, violatingPods []v1.Pod) (map[string]float64, error) {
	updates := make(map[string]float64)
	// If node diff is >= 0, return simple average because the adjustment will be surely not subceed min cpu of job
	// For each pod p in pods
	//		delta := min_cpu(p) - |simple_delta|
	//  	if delta < 0:
	//  		subceeding_count += 1;
	// 			map[name(p)] = cpu_limit(p) - min_cpu(p);
	//			ignore_partial += cpu_limit(p) - min_cpu(p);
	//		else:
	// 		 	not_subceeding = append(not_subceeding, p)
	// For each pod p in not_subceeding
	// 		map[name(p)] = (avg_diff(N) - ignore_partial) / (len(P) - subceeding_count)
	// For each k, v in map:
	//      patch(k, v)

	// For each violating pod, compute its own delta and keep track of: count of "specials" and sum of "special delta"
	// Recompute the delta excluding len(specials) and subtracting "special delta"
	// Now we have a delta for each node not subceeding which will be evenly distributed, and a lesser delta for each node
	// that would have its delta subceeding its min_job.
	avgNodeDiff := promclient.GetAvgInstantUsage(diffs.Data)
	if avgNodeDiff >= 0 {
		// Simple average, no need to take into consideration min
		simpleDelta := math.Abs(avgNodeDiff) / float64(len(violatingPods))
		for _, pod := range violatingPods {
			updates[pod.Name] = simpleDelta
		}
		return updates, nil
	}
	// In that case, we need to throttle some pods
	subceedingCount := 0
	ignorePartials := 0.0
	notSubceeding := make([]v1.Pod, 0)

	for _, pod := range violatingPods {
		minPodCpu, err := kubeclient.GetMinCpu(pod)
		if err.Error() == "job min annotation not found" {
			continue
		}
		if err != nil {
			return nil, err
		}
		podCpuLimitQuantity := pod.Spec.Containers[0].Resources.Limits.Cpu()
		podCpuLimit, err := kubeclient.ResourceQuantityToPercentage(cpuCounts, *podCpuLimitQuantity, diffs.NodeName)
		delta := minPodCpu - math.Abs(avgNodeDiff)
		if delta < 0 {
			delta = podCpuLimit - minPodCpu
			updates[pod.Name] = delta
			ignorePartials += delta
			subceedingCount += 1
		} else {
			notSubceeding = append(notSubceeding, pod)
		}
	}
	for _, v := range notSubceeding {
		updates[v.Name] = (avgNodeDiff - ignorePartials) / (float64(len(violatingPods) - subceedingCount))
	}

	return updates, nil
}
