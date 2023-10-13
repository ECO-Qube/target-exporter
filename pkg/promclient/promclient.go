package promclient

import (
	ctx "context"
	"fmt"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"strconv"
	"strings"
	"time"
)

// const nodeCpuPromQuery = `100 - 100 * avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[1m]))`
const nodeCpuPromQuery = `node_cpu_utilization`
const nodeCpuCountQuery = `count without(cpu, mode) (node_cpu_seconds_total{mode="idle"})`
const cpuDiffMetricName = "node_cpu_diff"

type Promclient struct {
	v1.API

	logger *zap.Logger
}

type InstantCpuUsage struct {
	Timestamp time.Time `json:"timestamp"`
	Usage     float64   `json:"data"` // CPU Usage in percentage 0-100
}

type NodeCpuUsage struct {
	NodeName string            `json:"nodeName"`
	Data     []InstantCpuUsage `json:"usage"`
}

func NewPromClient(client v1.API, logger *zap.Logger) *Promclient {
	return &Promclient{client, logger}
}

// GetCpuUsageByRangeSeconds returns an array of NodeCpuUsage for each nodes, one measurement per second between
// start and end.
// Payload Sample:
// [
//
//	{
//	    "nodeName": "ecoqube-dev-default-worker-topo-lx7l2-65d7746-bg5rp",
//	    "data": [{
//	        "timestamp": "2022-12-09T11:30:22+01:00",
//	        "usage": 6.553787878788029
//	    }, {
//	        "timestamp": "2022-12-09T11:30:22+01:00",
//	        "usage": 6.2871212121213205
//	    }, {
//	        "timestamp": "2022-12-09T11:30:22+01:00",
//	        "usage": 6.481060606060495
//	    }]
//	},
//	{
//	    "nodeName": "ecoqube-dev-default-worker-topo-pf1ls-15d1646-ax8vd",
//	    "data": [{
//	        "timestamp": "2022-12-09T11:30:22+01:00",
//	        "usage": 6.553787878788029
//	    }, {
//	        "timestamp": "2022-12-09T11:30:22+01:00",
//	        "usage": 6.2871212121213205
//	    }, {
//	        "timestamp": "2022-12-09T11:30:22+01:00",
//	        "usage": 6.481060606060495
//	    }]
//	}
//
// ]
func (p *Promclient) GetCpuUsageByRangeSeconds(start time.Time, end time.Time) ([]NodeCpuUsage, error) {
	r := v1.Range{
		Start: start,
		End:   end.Add(-time.Second), // Make last second non-inclusive
		Step:  time.Second,
	}
	result, warnings, err := p.QueryRange(ctx.Background(),
		nodeCpuPromQuery,
		r,
		v1.WithTimeout(5*time.Second))
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		p.logger.Warn(fmt.Sprintf("Prometheus Warnings: %v\n", warnings))
	}

	cpuUsagesPerNode := make([]NodeCpuUsage, 0)

	for _, entry := range result.(model.Matrix) {
		instants := make([]InstantCpuUsage, 0)
		for _, currentValue := range entry.Values {
			usage := currentValue.Value
			// Drop "fraction of second" from timestamp
			ts, err := strconv.ParseInt(strings.Split(currentValue.Timestamp.String(), ".")[0], 10, 64)
			if err != nil {
				return nil, err
			}
			instants = append(instants, InstantCpuUsage{
				Timestamp: time.Unix(ts, 0),
				Usage:     float64(usage),
			})
		}
		cpuUsagesPerNode = append(cpuUsagesPerNode, NodeCpuUsage{
			NodeName: string(model.LabelSet(entry.Metric)["instance"]),
			Data:     instants,
		})
	}

	return cpuUsagesPerNode, nil
}

// GetCurrentCpuDiff returns the difference between the current CPU usage and the target CPU usage
// for each node, based on the current time. It makes use of the "node_cpu_utilization" metric.
func (p *Promclient) GetCurrentCpuDiff() ([]NodeCpuUsage, error) {
	now := time.Now()
	result, warnings, err := p.Query(ctx.Background(),
		cpuDiffMetricName,
		now,
		v1.WithTimeout(5*time.Second))
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		p.logger.Warn(fmt.Sprintf("Prometheus Warnings: %v\n", warnings))
	}

	cpuUsagesPerNode := make([]NodeCpuUsage, 0)

	for _, entry := range result.(model.Vector) {
		// Assume there is only one value per each node (hence Values[0])
		instants := make([]InstantCpuUsage, 0)

		usage, err := strconv.ParseFloat(entry.Value.String(), 64)
		if err != nil {
			return nil, err
		}

		ts, err := strconv.ParseInt(now.String(), 10, 64)
		instants = append(instants, InstantCpuUsage{
			Timestamp: time.Unix(ts, 0),
			Usage:     usage,
		})

		cpuUsagesPerNode = append(cpuUsagesPerNode, NodeCpuUsage{
			NodeName: string(model.LabelSet(entry.Metric)["instance"]),
			Data:     instants,
		})
	}

	return cpuUsagesPerNode, nil

}

func (p *Promclient) GetNodeCpuDiff(nodeName string) (float64, error) {
	now := time.Now()
	result, warnings, err := p.Query(ctx.Background(),
		cpuDiffMetricName+`{instance="`+nodeName+`"}`,
		now,
		v1.WithTimeout(5*time.Second))
	if err != nil {
		return 0, err
	}
	if len(warnings) > 0 {
		p.logger.Warn(fmt.Sprintf("Prometheus Warnings: %v\n", warnings))
	}
	return strconv.ParseFloat(result.(model.Vector)[0].Value.String(), 64)
}

func (p *Promclient) GetCurrentEnergyConsumption() (map[string]float64, error) {
	result, warnings, err := p.Query(context.Background(), "fake_energy_consumption", time.Now(), v1.WithTimeout(5*time.Second))
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		p.logger.Warn(fmt.Sprintf("Prometheus Warnings: %v\n", warnings))
	}
	currentEnergyCons := make(map[string]float64)
	for _, entry := range result.(model.Vector) {
		energyCons, err := strconv.ParseFloat(entry.Value.String(), 64)
		if err != nil {
			return nil, err
		}
		nodeLabel := string(model.LabelSet(entry.Metric)["node_label"])
		currentEnergyCons[nodeLabel] = energyCons
	}
	//for _, entry := range result {
	//	fmt.Println(entry)
	//}
	return currentEnergyCons, nil
}

func (p *Promclient) GetCpuCounts() (map[string]int, error) {
	result, warnings, err := p.Query(ctx.Background(),
		nodeCpuCountQuery,
		time.Now(),
		v1.WithTimeout(5*time.Second))
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		p.logger.Warn(fmt.Sprintf("Prometheus Warnings: %v\n", warnings))
	}
	cpuCounts := make(map[string]int)
	for _, entry := range result.(model.Vector) {
		intValue, err := strconv.ParseInt(entry.Value.String(), 10, 32)
		if err != nil {
			return nil, err
		}
		cpuCounts[string(model.LabelSet(entry.Metric)["instance"])] = int(intValue)
	}

	return cpuCounts, nil
}

func GetAvgInstantUsage(usages []InstantCpuUsage) float64 {
	var sum float64
	for _, usage := range usages {
		sum += usage.Usage
	}
	return sum / float64(len(usages))
}
