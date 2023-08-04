package selfdriving

import (
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"sync/atomic"
	"time"
)

type SelfDriving struct {
	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient

	pause             chan bool
	isRunning         bool
	isCommandOnFlight atomic.Bool
	initialized       bool
}

// TODO: Since operations on on isCommandOnFlight are not atomic, although there are no data races, this does not work for now
// if two functions check isCommandOnFlight, continue executing, and one of the two sets it to true, the other will still be running...
// solution: use a mutex (but then how to check if the mutex is locked without blocking?) or find how to use channels correctly
func NewSelfDriving() *SelfDriving {
	return &SelfDriving{pause: make(chan bool), isRunning: false, isCommandOnFlight: atomic.Bool{}, initialized: false}
}

func (s *SelfDriving) Start() error {
	if s.isCommandOnFlight.Load() {
		fmt.Println("debug: command on flight")
		return fmt.Errorf("command on flight")
	}

	fmt.Println("Starting controller")

	s.isCommandOnFlight.Store(true)
	if !s.initialized {
		s.initialized = true
		s.run()
	}
	s.pause <- false
	s.isCommandOnFlight.Store(false)

	return nil
}

func (s *SelfDriving) Stop() error {
	if s.isCommandOnFlight.Load() {
		fmt.Println("debug: command on flight")
		return fmt.Errorf("command on flight")
	}

	fmt.Println("Stopping controller")

	s.isCommandOnFlight.Store(true)
	s.pause <- true
	s.isCommandOnFlight.Store(false)

	return nil
}

func (s *SelfDriving) run() {
	go func() {
		for {
			if s.isCommandOnFlight.Load() || !s.initialized {
				command := <-s.pause
				if !command {
					s.isRunning = true
				} else {
					s.isRunning = false
					fmt.Println("debug: pausing until new command")
				}
			}

			// My logic here
			fmt.Println("debug: loop")
			time.Sleep(2 * time.Second)
		}
	}()
}
