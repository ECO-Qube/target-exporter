package promclient

import (
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"go.uber.org/zap"
)

type Promclient struct {
	v1.API

	logger *zap.Logger
}

func NewPromClient(client v1.API, logger *zap.Logger) *Promclient {
	return &Promclient{client, logger}
}
