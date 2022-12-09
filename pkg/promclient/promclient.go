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

// GetCpuUsageByRangeSeconds returns an array of NodeCpuUsage structs for each nodes, one measurement per second between
// start and end.
func (p *Promclient) GetCpuUsageByRangeSeconds(start time.Time, end time.Time) ([]NodeCpuUsage, error) {

  // Sample:
  /*
     [
        {
             "nodeName": "ecoqube-dev-default-worker-topo-lx7l2-65d7746-bg5rp",
             "data": [{
                 "timestamp": "2022-12-09T11:30:22+01:00",
                 "usage": 6.553787878788029
             }, {
                 "timestamp": "2022-12-09T11:30:22+01:00",
                 "usage": 6.2871212121213205
             }, {
                 "timestamp": "2022-12-09T11:30:22+01:00",
                 "usage": 6.481060606060495
             }]
         },
         {
             "nodeName": "ecoqube-dev-default-worker-topo-pf1ls-15d1646-ax8vd",
             "data": [{
                 "timestamp": "2022-12-09T11:30:22+01:00",
                 "usage": 6.553787878788029
             }, {
                 "timestamp": "2022-12-09T11:30:22+01:00",
                 "usage": 6.2871212121213205
             }, {
                 "timestamp": "2022-12-09T11:30:22+01:00",
                 "usage": 6.481060606060495
             }]
         }
     ]
  */

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
    // Assume there is only one value per each node (hence Values[0])
    instants := make([]InstantCpuUsage, 0)
    for _, currentValue := range entry.Values {
      // Drop "fraction of second" from timestamp
      ts, err := strconv.ParseInt(strings.Split(currentValue.Timestamp.String(), ".")[0], 10, 64)
      if err != nil {
        return nil, err
      }
      usage, err := strconv.ParseFloat(currentValue.Value.String(), 64)
      if err != nil {
        return nil, err
      }

      instants = append(instants, InstantCpuUsage{
        Timestamp: time.Unix(ts, 0),
        Usage:     usage,
      })
    }
    cpuUsagesPerNode = append(cpuUsagesPerNode, NodeCpuUsage{
      NodeName: string(model.LabelSet(entry.Metric)["instance"]),
      Data:     instants,
    })
  }

  return cpuUsagesPerNode, nil
}
