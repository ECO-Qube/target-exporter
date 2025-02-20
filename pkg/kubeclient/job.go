package kubeclient

import (
	"fmt"
	"github.com/google/uuid"
	v1batch "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/yaml"
	"strconv"
	"strings"
	"time"
)

type HardwareTarget string

const HardwareTypeAnnotation = "ecoqube.eu/hardware-type"
const JobStartDateAnnotation = "ecoqube.eu/start"
const JobMinCpuLimitAnnotation = "ecoqube.eu/min-cpu-limit"

const (
	CpuIntensive     HardwareTarget = "cpu"
	StorageIntensive HardwareTarget = "storage"
	MemoryIntensive  HardwareTarget = "memory"
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
	WithWorkloadType(HardwareTarget) JobBuilder
	WithLength(time.Duration) JobBuilder
	WithNodeSelector(string) JobBuilder
	WithStartDate(time.Time) JobBuilder
	WithMinCpuLimit(float64) JobBuilder
	Build() (*StressJob, error)
}

type Job interface {
	GetName() string
	GetCpuLimit() resource.Quantity
	GetCpuCount() int
	GetWorkloadType() HardwareTarget
	GetNodeSelector() map[string]string
	WithMinCpuLimit(resource.Quantity) JobBuilder
	RenderK8sJob() (BaseJob, error)
}

type BaseJob struct {
	name         string
	cpuLimit     resource.Quantity
	cpuCount     int
	workloadType HardwareTarget
	nodeSelector map[string]string
	startDate    time.Time
	minCpuLimit  float64

	k8sJob *v1batch.Job
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

func (builder *StressJobBuilder) WithWorkloadType(workloadType HardwareTarget) JobBuilder {
	builder.job.workloadType = workloadType
	return builder
}

func (builder *StressJobBuilder) WithLength(length time.Duration) JobBuilder {
	builder.job.length = length
	return builder
}

func (builder *StressJobBuilder) WithNodeSelector(nodeName string) JobBuilder {
	builder.job.nodeSelector = map[string]string{"kubernetes.io/hostname": nodeName}
	return builder
}

func (builder *StressJobBuilder) WithStartDate(startDate time.Time) JobBuilder {
	builder.job.startDate = startDate
	return builder
}

func (builder *StressJobBuilder) WithMinCpuLimit(minCpuLimit float64) JobBuilder {
	builder.job.minCpuLimit = minCpuLimit
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

func (s *StressJob) GetWorkloadType() HardwareTarget {
	return s.workloadType
}

func (s *StressJob) RenderK8sJob() (*v1batch.Job, error) {
	var job *v1batch.Job
	err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(StressJobPrototype), len(StressJobPrototype)).Decode(&job)
	if err != nil {
		return nil, err
	}

	deadline := int64(s.length.Seconds())
	job.Name = s.name
	// TODO: What if there are multiple containers?
	job.ObjectMeta.Name = s.name
	labels := job.Spec.Template.ObjectMeta.GetLabels()
	labels["app"] = s.name
	job.Spec.Template.ObjectMeta.SetLabels(labels)
	job.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"] = s.cpuLimit
	job.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"] = *resource.NewMilliQuantity(s.cpuLimit.MilliValue()/4, resource.DecimalSI)
	job.Spec.Template.Spec.Containers[0].Env[0].Name = "MAX_CPU_CORES"
	job.Spec.Template.Spec.Containers[0].Env[0].Value = strconv.Itoa(s.cpuCount)
	job.Spec.Template.Spec.Containers[0].Env[1].Name = "STRESS_SYSTEM_FOR"
	job.Spec.Template.Spec.Containers[0].Env[1].Value = fmt.Sprintf("%.0fm", s.length.Minutes())
	// Ensure deadline
	job.Spec.ActiveDeadlineSeconds = &deadline
	if s.nodeSelector != nil {
		job.Spec.Template.Spec.NodeSelector = s.nodeSelector
	}
	if workloadType := string(s.workloadType); strings.TrimSpace(workloadType) != "" {
		// TODO: Needs quick testing
		if job.Spec.Template.Spec.NodeSelector == nil {
			job.Spec.Template.Spec.NodeSelector = map[string]string{HardwareTypeAnnotation: workloadType}
		} else {
			job.Spec.Template.Spec.NodeSelector[HardwareTypeAnnotation] = workloadType
		}
	}
	// If startDate is after now, add annotation
	if s.startDate.After(time.Now()) {
		t := true
		job.Spec.Suspend = &t
		annotations := job.Annotations
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[JobStartDateAnnotation] = s.startDate.Format(time.RFC3339)
		job.SetAnnotations(annotations)
	}

	// Add min cpu annotation
	annotations := job.Spec.Template.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[JobMinCpuLimitAnnotation] = fmt.Sprintf("%f", s.minCpuLimit)
	job.Spec.Template.SetAnnotations(annotations)

	return job, nil
}

func generateJobName(jobCpuLimit string) string {
	return jobCpuLimit + "-cpu-stresstest-" + uuid.New().String()[0:8]
}

func MinutesToDuration(minutes int) time.Duration {
	return time.Duration(minutes * int(time.Minute))
}

func GetMinCpu(pod v1.Pod) (float64, error) {
	// Get min cpu limit from annotation
	minCpuLimit, ok := pod.Annotations[JobMinCpuLimitAnnotation]
	if !ok {
		return 0, fmt.Errorf("job min annotation not found")
	}
	// Parse annotation to float
	minCpuLimitFloat, err := strconv.ParseFloat(minCpuLimit, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse min cpu limit")
	}
	return minCpuLimitFloat, nil
}
