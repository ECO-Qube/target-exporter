package promclient

import (
  ctx "context"
  "fmt"
  v1 "github.com/prometheus/client_golang/api/prometheus/v1"
  "go.uber.org/zap"
  "os"
  "time"
)

type Promclient struct {
  v1.API

  logger *zap.Logger
}

func NewPromClient(client v1.API, logger *zap.Logger) *Promclient {
  return &Promclient{client, logger}
}

type QueryResult struct {
  Data struct {
    ResultType string `json:"resultType"`
    Result     []struct {
      Metric struct {
        __name__ string `json:"__name__"`
        job      string `json:"job"`
        instance string `json:"instance"`
      } `json:"metric"`
      Value []interface{} `json:"value"`
    } `json:"result"`
  } `json:"data"`
}

func (p *Promclient) GetCurrentCpuUsagePerNode() {
  r := v1.Range{
    Start: time.Now(),
    End:   time.Now().Add(time.Second),
    Step:  time.Minute,
  }
  result, warnings, err := p.QueryRange(ctx.Background(),
    "100 - 100 * avg by (instance) (rate(node_cpu_seconds_total{mode=\"idle\"}[1m]))",
    r,
    v1.WithTimeout(5*time.Second))
  if err != nil {
    fmt.Printf("Error querying Prometheus: %v\n", err)
    os.Exit(1)
  }
  if len(warnings) > 0 {
    fmt.Printf("Warnings: %v\n", warnings)
  }
  fmt.Printf("Result:\n%v\n", result)
}
