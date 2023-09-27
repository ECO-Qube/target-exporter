package scheduling

import (
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	Start = "start"
	Stop  = "stop"
)

type ConcurrentStrategy interface {
	Start()
	Stop()
	IsRunning()
}

type BaseConcurrentStrategy struct {
	strategyName string
	logger       *zap.Logger
	reconcile    func() error

	mu          *sync.Mutex
	initialized bool
	isRunning   bool
	startStop   chan string
}

func NewBaseConcurrentStrategy(strategyName string, reconcile func() error, logger *zap.Logger) *BaseConcurrentStrategy {
	return &BaseConcurrentStrategy{
		strategyName: strategyName,
		logger:       logger.With(zap.String("strategyName", strategyName)),
		reconcile:    reconcile,

		mu:          &sync.Mutex{},
		initialized: false,
		isRunning:   false,
		startStop:   make(chan string),
	}
}

func (c *BaseConcurrentStrategy) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("starting strategy")
	if !c.initialized {
		c.initialized = true
		c.run()
	}
	c.startStop <- Start
}

func (c *BaseConcurrentStrategy) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Debug("stopping strategy")
	c.startStop <- Stop
}

func (c *BaseConcurrentStrategy) IsRunning() bool {
	return c.isRunning
}

func (c *BaseConcurrentStrategy) run() {
	go func() {
		run := false
		for {
			select {
			case command, ok := <-c.startStop:
				if ok {
					if command == Start {
						run = true
						c.isRunning = true
					} else {
						run = false
						c.isRunning = false
					}
				} else {
					c.logger.Info("startStop channel closed")
					break
				}
			default:
				//c.logger.Debug("startStop channel empty, continuing", zap.Bool("run", run))
			}
			if run {
				err := c.reconcile()
				if err != nil {
					c.logger.Error("error while reconciling", zap.Error(err))
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()
}
