package kubeclient

import (
	"fmt"
	"github.com/google/uuid"
	v1batch "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/yaml"
	"strconv"
	"strings"
	"time"
)

type WorkloadType string

const WorkloadTypeAnnotation = "ecoqube.eu/wkld-type"

const (
	CpuIntensive     WorkloadType = "cpu"
	StorageIntensive WorkloadType = "storage"
	MemoryIntensive  WorkloadType = "memory"
)

const StressJobPrototype = `
apiVersion: batch/v1
kind: Job
metadata:
  name: cpu-stress-job-proto
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: cpu-stress-job-proto
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
        - name: cpu-stress-job-proto
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

type Job interface {
	GetName() string
	GetCpuLimit() resource.Quantity
	GetCpuCount() int
	GetJobType() WorkloadType
	GetK8sJob() (Job, error)
}

type StressJob struct {
	Name         string
	CpuLimit     resource.Quantity
	CpuCount     int
	Length       time.Duration // Normally Jobs don't have a predefined length, but this is a stress test
	WorkloadType WorkloadType
	k8sJob       *v1batch.Job
}

func NewStressJob(jobCpuLimit int, cpuCount int, jobLength time.Duration, workloadType WorkloadType) *StressJob {
	limit := resource.NewMilliQuantity(int64(jobCpuLimit)*10, resource.DecimalSI)
	return &StressJob{Name: generateJobName(strconv.Itoa(jobCpuLimit)), CpuLimit: *limit, CpuCount: cpuCount, Length: jobLength, WorkloadType: workloadType}
}

func (s *StressJob) GetName() string {
	return s.Name
}

func (s *StressJob) GetCpuLimit() resource.Quantity {
	return s.CpuLimit
}

func (s *StressJob) GetCpuCount() int {
	return s.CpuCount
}

func (s *StressJob) GetK8sJob() (*v1batch.Job, error) {
	return renderK8sStressJob(s.Name, s.CpuLimit, s.CpuCount, s.Length, s.WorkloadType)
}

func (s *StressJob) GetWorkloadType() WorkloadType {
	return s.WorkloadType
}

func renderK8sStressJob(name string, cpuLimit resource.Quantity, cpuCount int, length time.Duration, workloadType WorkloadType) (*v1batch.Job, error) {
	var job *v1batch.Job
	err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(StressJobPrototype), len(StressJobPrototype)).Decode(&job)
	if err != nil {
		return nil, err
	}

	job.Name = name
	// TODO: What if there are multiple containers?
	job.ObjectMeta.Name = name
	job.Spec.Template.ObjectMeta.SetLabels(map[string]string{"app": name})
	job.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"] = cpuLimit
	job.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"] = cpuLimit
	job.Spec.Template.Spec.Containers[0].Env[0].Name = "MAX_CPU_CORES"
	job.Spec.Template.Spec.Containers[0].Env[0].Value = strconv.Itoa(cpuCount)
	job.Spec.Template.Spec.Containers[0].Env[1].Name = "STRESS_SYSTEM_FOR"
	job.Spec.Template.Spec.Containers[0].Env[1].Value = fmt.Sprintf("%.0fm", length.Minutes())
	// TODO: What if NodeSelector was already populated?
	if workloadType := string(workloadType); strings.TrimSpace(workloadType) != "" {
		job.Spec.Template.Spec.NodeSelector = map[string]string{WorkloadTypeAnnotation: workloadType}
	}

	return job, nil
}

func generateJobName(jobCpuLimit string) string {
	fmt.Println(jobCpuLimit)
	return jobCpuLimit + "-cpu-stresstest-" + uuid.New().String()[0:8]
}
