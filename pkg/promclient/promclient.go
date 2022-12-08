package promclient

import (
	ctx "context"
	"fmt"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
	"time"
)

type Promclient struct {
	v1.API

	logger *zap.Logger
}

const CpuNodeQuery = `100 - 100 * avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[1m]))`

func NewPromClient(client v1.API, logger *zap.Logger) *Promclient {
	return &Promclient{client, logger}
}

func (p *Promclient) GetCurrentCpuUsagePerNode() error {
	r := v1.Range{
		Start: time.Now(),
		End:   time.Now().Add(time.Second),
		Step:  time.Minute,
	}
	result, warnings, err := p.QueryRange(ctx.Background(),
		CpuNodeQuery,
		r,
		v1.WithTimeout(5*time.Second))
	if err != nil {
		return err
	}
	if len(warnings) > 0 {
		p.logger.Warn(fmt.Sprintf("Prometheus Warnings: %v\n", warnings))
	}

	mapData := make(map[model.Time]model.SampleValue)

	for _, val := range result.(model.Matrix)[0].Values {
		mapData[val.Timestamp] = val.Value
	}

	fmt.Printf("Result:\n%v\n", len(mapData))

	return nil
}
