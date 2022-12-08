package promclient

import (
	ctx "context"
	"fmt"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
	"strconv"
	"strings"
	"time"
)

const nodeCpuPromQuery = `100 - 100 * avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[1m]))`

type Promclient struct {
	v1.API

	logger *zap.Logger
}

type NodeCpuUsage struct {
	NodeName  string    `json:"nodeName"`
	Timestamp time.Time `json:"timestamp"`
	Usage     float64   `json:"usage"` // CPU Usage in percentage 0-100
}

func NewPromClient(client v1.API, logger *zap.Logger) *Promclient {
	return &Promclient{client, logger}
}

func (p *Promclient) GetCurrentCpuUsagePerNode() ([]NodeCpuUsage, error) {
	r := v1.Range{
		Start: time.Now(),
		End:   time.Now().Add(time.Second),
		Step:  time.Minute,
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
		// Assume there is only one value per each node (hence Values[0])
		// Drop "fraction of second" from timestamp
		currentValue := entry.Values[0]
		ts, err := strconv.ParseInt(strings.Split(currentValue.Timestamp.String(), ".")[0], 10, 64)
		if err != nil {
			return nil, err
		}
		usage, err := strconv.ParseFloat(currentValue.Value.String(), 64)
		if err != nil {
			return nil, err
		}
		cpuUsagesPerNode = append(cpuUsagesPerNode, NodeCpuUsage{
			NodeName:  string(model.LabelSet(entry.Metric)["instance"]),
			Timestamp: time.Unix(ts, 0),
			Usage:     usage,
		})

		fmt.Println(cpuUsagesPerNode)
	}

	return cpuUsagesPerNode, nil
}
