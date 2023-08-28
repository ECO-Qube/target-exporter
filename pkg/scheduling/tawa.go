package scheduling

import (
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"go.uber.org/zap"
	"sync"
)

type TawaStrategy struct {
	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient
	targets    map[string]*Target

	logger      *zap.Logger
	startStop   chan string
	initialized bool
	mu          sync.Mutex
	skipForNow  SkipList
	IsEnabled   bool
}

func NewTawaStrategy(kubeClient *kubeclient.Kubeclient, promClient *promclient.Promclient, logger *zap.Logger, targets map[string]*Target) *TawaStrategy {
	return &TawaStrategy{
		kubeClient: kubeClient,
		promClient: promClient,
		logger:     logger,
		targets:    targets,

		startStop:   make(chan string),
		initialized: false,
		IsEnabled:   false,
	}
}

func (s *TawaStrategy) Enable() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.IsEnabled = true
}

func (s *TawaStrategy) Disable() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.IsEnabled = false
}
