package kubeclient

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	v1batch "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"sort"
	"strings"
	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

// TODO: Deploy exporter in TAS-enabled cluster
//const stressTestJob25 = `
//apiVersion: batch/v1
//kind: CronJob
//metadata:
//  name: 25-cpu-stresstest-cron
//spec:
//  schedule: "*/1 * * * *" # every minute
//  jobTemplate:
//    spec:
//      template:
//        metadata:
//          labels:
//            app: 25-cpu-stresstest-cron
//            telemetry-policy: cpu-diff-policy
//        spec:
//          affinity:
//            nodeAffinity:
//              requiredDuringSchedulingIgnoredDuringExecution:
//                nodeSelectorTerms:
//                  - matchExpressions:
//                      - key: cpu-diff-policy
//                        operator: NotIn
//                        values:
//                          - violating
//          containers:
//          - name: 25-cpu-stresstest-cron
//            image: petarmaric/docker.cpu-stress-test
//            imagePullPolicy: IfNotPresent
//            env:
//              - name: MAX_CPU_CORES
//                value: "2"
//              - name: STRESS_SYSTEM_FOR
//                value: "5m"
//            resources:
//              requests:
//                cpu: "250m"
//              limits:
//                cpu: "250m"
//                telemetry/scheduling: "1"
//          restartPolicy: Never
//      parallelism: 1
//      completions: 1
//`

const stressTestJob25 = `
apiVersion: batch/v1
kind: Job
metadata:
  name: 25-cpu-stresstest-cron
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: 25-cpu-stresstest-cron
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: cpu-diff-policy
                    operator: NotIn
                    values:
                      - violating
      containers:
        - name: 25-cpu-stresstest-cron
          image: petarmaric/docker.cpu-stress-test
          imagePullPolicy: IfNotPresent
          env:
            - name: MAX_CPU_CORES
              value: '1'
            - name: STRESS_SYSTEM_FOR
              value: 1m
          resources:
            requests:
              cpu: 250m
            limits:
              cpu: 250m
      restartPolicy: Never
  backoffLimit: 4
`

var policy = metav1.DeletePropagationForeground

type Kubeclient struct {
	*kubernetes.Clientset

	logger *zap.Logger
	ns     string
}

func NewKubeClient(client *kubernetes.Clientset, logger *zap.Logger) *Kubeclient {
	return &Kubeclient{client, logger, "default"}
}

// https://github.com/kubernetes/client-go/blob/master/examples/out-of-cluster-client-configuration/main.go
func (kc *Kubeclient) GetPodsInNamespace() (*v1.PodList, error) {
	// TODO: Make namespace configurable or get via label selection
	pods, err := kc.CoreV1().Pods(kc.ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		kc.logger.Error("Error getting pods", zap.Error(err))
		return nil, err
	}
	return pods, nil

	// Examples for error handling:
	// - Use helper functions like e.g. errors.IsNotFound()
	// - And/or cast to StatusError and use its properties like e.g. ErrStatus.Message
	//namespace := kc.ns
	//pod := "example-xxxxx"
	//_, err = clientset.CoreV1().Pods(namespace).Get(context.TODO(), pod, metav1.GetOptions{})
	//if errors.IsNotFound(err) {
	//	fmt.Printf("Pod %s in namespace %s not found\n", pod, namespace)
	//} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
	//	fmt.Printf("Error getting pod %s in namespace %s: %v\n",
	//		pod, namespace, statusError.ErrStatus.Message)
	//} else if err != nil {
	//	panic(err.Error())
	//} else {
	//	fmt.Printf("Found pod %s in namespace %s\n", pod, namespace)
	//}
}

// SpawnNewWorkload creates a new stress test workload
func (kc *Kubeclient) SpawnNewWorkload() error {
	// TODO: Parametrize...
	var job *v1batch.Job
	err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(stressTestJob25), len(stressTestJob25)).Decode(&job)
	if err != nil {
		kc.logger.Error("Error decoding yaml", zap.Error(err))
		return err
	}
	kc.logger.Info("Spawning cronjob", zap.String("name", job.Name))

	job.Name = job.Name + "-" + uuid.New().String()[0:8]

	job, err = kc.BatchV1().Jobs(kc.ns).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		fmt.Println(err.Error())
		kc.logger.Error("Error from K8s API when creating cronjob resource", zap.Error(err))
		return err
	}
	kc.logger.Info("Cronjob created", zap.String("name", job.Name))

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

func isOwnerPresent(ownerRefs []metav1.OwnerReference, ownerName string, kind string) bool {
	for _, ownerRef := range ownerRefs {
		if ownerRef.Name == ownerName && ownerRef.Kind == kind {
			return true
		}
	}
	return false
}
