package kubeclient

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	v1batch "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
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

type Kubeclient struct {
	*kubernetes.Clientset

	logger *zap.Logger
}

func NewKubeClient(client *kubernetes.Clientset, logger *zap.Logger) *Kubeclient {
	return &Kubeclient{client, logger}
}

// https://github.com/kubernetes/client-go/blob/master/examples/out-of-cluster-client-configuration/main.go
func (kubeclient *Kubeclient) GetPodsInNamespace() (*v1.PodList, error) {
	// TODO: Make namespace configurable or get via label selection
	pods, err := kubeclient.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		kubeclient.logger.Error("Error getting pods", zap.Error(err))
		return nil, err
	}
	return pods, nil

	// Examples for error handling:
	// - Use helper functions like e.g. errors.IsNotFound()
	// - And/or cast to StatusError and use its properties like e.g. ErrStatus.Message
	//namespace := "default"
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
func (kubeclient *Kubeclient) SpawnNewWorkload() error {
	// TODO: Parametrize...
	var job *v1batch.Job
	err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(stressTestJob25), len(stressTestJob25)).Decode(&job)
	if err != nil {
		kubeclient.logger.Error("Error decoding yaml", zap.Error(err))
		return err
	}
	kubeclient.logger.Info("Spawning cronjob", zap.String("name", job.Name))

	job.Name = job.Name + "-" + uuid.New().String()[0:8]

	job, err = kubeclient.BatchV1().Jobs("default").Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		fmt.Println(err.Error())
		kubeclient.logger.Error("Error from K8s API when creating cronjob resource", zap.Error(err))
		return err
	}
	kubeclient.logger.Info("Cronjob created", zap.String("name", job.Name))

	return nil
}
