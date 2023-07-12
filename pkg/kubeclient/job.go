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
        telemetry-policy: schedule-until-at-capacity
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: schedule-until-at-capacity
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
              telemetry/scheduling: "1"
      restartPolicy: Never
  backoffLimit: 4
`

type JobBuilder interface {
	WithName(string) JobBuilder
	WithCpuLimit(resource.Quantity) JobBuilder
	WithCpuCount(int) JobBuilder
	WithWorkloadType(WorkloadType) JobBuilder
	WithLength(time.Duration) JobBuilder
	Build() (*StressJob, error)
}

type Job interface {
	GetName() string
	GetCpuLimit() resource.Quantity
	GetCpuCount() int
	GetWorkloadType() WorkloadType
	RenderK8sJob() (BaseJob, error)
}

type BaseJob struct {
	name         string
	cpuLimit     resource.Quantity
	cpuCount     int
	workloadType WorkloadType
	k8sJob       *v1batch.Job
}

type StressJob struct {
	BaseJob
	length time.Duration
}

type StressJobBuilder struct {
	job *StressJob
}

func NewConcreteStressJobBuilder() *StressJobBuilder {
	return &StressJobBuilder{job: &StressJob{}}
}

func (builder *StressJobBuilder) WithName(name string) JobBuilder {
	builder.job.name = name
	return builder
}

func (builder *StressJobBuilder) WithCpuLimit(cpuLimit resource.Quantity) JobBuilder {
	builder.job.cpuLimit = cpuLimit
	return builder
}

func (builder *StressJobBuilder) WithCpuCount(cpuCount int) JobBuilder {
	builder.job.cpuCount = cpuCount
	return builder
}

func (builder *StressJobBuilder) WithWorkloadType(workloadType WorkloadType) JobBuilder {
	builder.job.workloadType = workloadType
	return builder
}

func (builder *StressJobBuilder) WithLength(length time.Duration) JobBuilder {
	builder.job.length = length
	return builder
}

func (builder *StressJobBuilder) Build() (*StressJob, error) {
	// TODO: Validation
	if builder.job.name == "" {
		builder.job.name = generateJobName(builder.job.cpuLimit.String())
	}
	return builder.job, nil
}

func (s *StressJob) GetName() string {
	return s.name
}

func (s *StressJob) GetCpuLimit() resource.Quantity {
	return s.cpuLimit
}

func (s *StressJob) SetCpuLimit(limit resource.Quantity) {
	s.cpuLimit = limit
}

func (s *StressJob) GetCpuCount() int {
	return s.cpuCount
}

func (s *StressJob) GetWorkloadType() WorkloadType {
	return s.workloadType
}

func (s *StressJob) RenderK8sJob() (*v1batch.Job, error) {
	var job *v1batch.Job
	err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(StressJobPrototype), len(StressJobPrototype)).Decode(&job)
	if err != nil {
		return nil, err
	}

	job.Name = s.name
	// TODO: What if there are multiple containers?
	job.ObjectMeta.Name = s.name
	labels := job.Spec.Template.ObjectMeta.GetLabels()
	labels["app"] = s.name
	job.Spec.Template.ObjectMeta.SetLabels(labels)
	job.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"] = s.cpuLimit
	job.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"] = s.cpuLimit
	job.Spec.Template.Spec.Containers[0].Env[0].Name = "MAX_CPU_CORES"
	job.Spec.Template.Spec.Containers[0].Env[0].Value = strconv.Itoa(s.cpuCount)
	job.Spec.Template.Spec.Containers[0].Env[1].Name = "STRESS_SYSTEM_FOR"
	job.Spec.Template.Spec.Containers[0].Env[1].Value = fmt.Sprintf("%.0fm", s.length.Minutes())
	// TODO: What if NodeSelector was already populated?
	if workloadType := string(s.workloadType); strings.TrimSpace(workloadType) != "" {
		job.Spec.Template.Spec.NodeSelector = map[string]string{WorkloadTypeAnnotation: workloadType}
	}

	return job, nil
}

func generateJobName(jobCpuLimit string) string {
	fmt.Println(jobCpuLimit)
	return jobCpuLimit + "-cpu-stresstest-" + uuid.New().String()[0:8]
}
