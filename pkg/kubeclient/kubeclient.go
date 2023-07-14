package kubeclient

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	v1batch "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sort"
	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

const (
	JobSelectorLabel = "batch.kubernetes.io/job-name"
)

var policy = metav1.DeletePropagationForeground

type Kubeclient struct {
	*kubernetes.Clientset

	logger *zap.Logger
	ns     string
}

func NewKubeClient(client *kubernetes.Clientset, logger *zap.Logger) *Kubeclient {
	return &Kubeclient{client, logger, "default"}
}

func (kc *Kubeclient) PatchCpuLimit(limit resource.Quantity, podName string) error {
	kc.logger.Info("Patching Job limit", zap.String("name", podName),
		zap.String("newLimit", limit.String()))
	// Then patch the container's CPU limit
	patch := fmt.Sprintf(`{"spec":{"containers":[{"name":"cpu-stress-job-proto", "resources":{"requests":{"cpu":"%s"}, "limits": {"cpu": "%s"}}}]}}`, limit.String(), limit.String())
	patchedPod, err := kc.CoreV1().Pods(kc.ns).Patch(context.TODO(), podName, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		kc.logger.Error("Error patching pod", zap.Error(err))
		return err
	}
	kc.logger.Info("Pod patched successfully", zap.String("name", patchedPod.Name))
	return nil
}

func (kc *Kubeclient) GetPodsInNamespace() (*v1.PodList, error) {
	// https://github.com/kubernetes/client-go/blob/master/examples/out-of-cluster-client-configuration/main.go
	// TODO: Make namespace configurable or get via label selection
	pods, err := kc.CoreV1().Pods(kc.ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		kc.logger.Error("Error getting pods", zap.Error(err))
		return nil, err
	}
	return pods, nil
}

// SpawnNewWorkload creates a new stress test workload
func (kc *Kubeclient) SpawnNewWorkload(job *StressJob) error {
	k8sJob, err := job.RenderK8sJob()
	if err != nil {
		kc.logger.Error("Error getting K8s Job", zap.Error(err))
		return err
	}
	kc.logger.Info("Spawning Job", zap.String("name", job.name))

	resultingJob, err := kc.BatchV1().Jobs(kc.ns).Create(context.TODO(), k8sJob, metav1.CreateOptions{})
	if err != nil {
		fmt.Println(err.Error())
		kc.logger.Error("Error from K8s API when creating Job resource", zap.Error(err))
		return err
	}
	kc.logger.Info("Job created successfully", zap.String("name", resultingJob.Name))

	return nil
}

func (kc *Kubeclient) ClearCompletedWorkloads() (done bool, err error) {
	kc.logger.Info("Clearing completed workloads")
	jobs, err := kc.BatchV1().Jobs(kc.ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		kc.logger.Error("Error getting Jobs", zap.Error(err))
		return false, err
	}
	for _, job := range jobs.Items {
		if job.Status.Active == 0 && job.Status.Succeeded > 0 {
			kc.logger.Info("Deleting completed Job", zap.String("name", job.Name))
			err = kc.BatchV1().Jobs(kc.ns).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
			if err != nil {
				kc.logger.Error("Error deleting Job", zap.Error(err))
				return false, err
			}
		}
	}
	pods, err := kc.CoreV1().Pods(kc.ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		kc.logger.Error("Error getting jobs", zap.Error(err))
		return false, err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodSucceeded {
			kc.logger.Info("Deleting completed Pod", zap.String("name", pod.Name))
			err = kc.CoreV1().Pods(kc.ns).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				kc.logger.Error("Error deleting job", zap.Error(err))
				return false, err
			} else {
				done = true
			}
		}
	}
	return done, nil
}

func (kc *Kubeclient) DeletePendingWorkload() (done bool, err error) {
	kc.logger.Info("Delete pending workload")
	jobs, err := kc.BatchV1().Jobs(kc.ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		kc.logger.Error("Error getting Jobs", zap.Error(err))
		return false, err
	}
	// Set aside Jobs with active pods
	var candidateJobs []v1batch.Job
	for _, job := range jobs.Items {
		if job.Status.Succeeded == 0 {
			candidateJobs = append(candidateJobs, job)
		}
	}

	if len(candidateJobs) == 0 {
		return false, nil
	}

	// Sort candidate Jobs by creation time
	sort.Slice(candidateJobs, func(i, j int) bool {
		return candidateJobs[i].CreationTimestamp.Before(&candidateJobs[j].CreationTimestamp)
	})

	pods, err := kc.CoreV1().Pods(kc.ns).List(context.TODO(), metav1.ListOptions{FieldSelector: "status.phase=Pending"})
	if err != nil {
		kc.logger.Error("Error getting Pods", zap.Error(err))
		return false, err
	}
	// Get the oldest Job and all Pending Pods, if Pod has owner a candidate Job, delete it and return, else go to next oldest Job and repeat
	for _, candidateJob := range candidateJobs {
		for _, pod := range pods.Items {
			if isOwnerPresent(pod.OwnerReferences, candidateJob.Name, "Job") {
				kc.logger.Info("Deleting Job with pending Pods", zap.String("name", candidateJob.Name))
				err = kc.BatchV1().Jobs(kc.ns).Delete(context.TODO(), candidateJob.Name, metav1.DeleteOptions{
					PropagationPolicy: &policy,
				})
				if err != nil {
					kc.logger.Error("Error deleting Job", zap.Error(err))
					return false, err
				}
				return true, nil
			}
		}
	}
	return false, nil
}

func (kc *Kubeclient) IsNodeNameValid(name string) bool {
	_, err := kc.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		kc.logger.Error("Error getting node", zap.Error(err))
		return false
	}
	if errors.IsNotFound(err) {
		kc.logger.Error("Node not found", zap.Error(err))
		return false
	}
	return true
}

func (kc *Kubeclient) GetPodNodeName(podName string) (string, error) {
	pod, err := kc.CoreV1().Pods(kc.ns).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		kc.logger.Error("Error getting Job", zap.Error(err))
		return "", err
	}
	return pod.Spec.NodeName, nil
}

func isOwnerPresent(ownerRefs []metav1.OwnerReference, ownerName string, kind string) bool {
	for _, ownerRef := range ownerRefs {
		if ownerRef.Name == ownerName && ownerRef.Kind == kind {
			return true
		}
	}
	return false
}
