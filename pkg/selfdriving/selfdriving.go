package selfdriving

import (
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	"git.helio.dev/eco-qube/target-exporter/pkg/promclient"
	"sync"
	"time"
)

const Start = "start"
const Stop = "stop"

type SelfDriving struct {
	kubeClient *kubeclient.Kubeclient
	promClient *promclient.Promclient

	startStop   chan string
	initialized bool
	mu          sync.Mutex
}

// TODO: Since operations on on isCommandOnFlight are not atomic, although there are no data races, this does not work for now
// if two functions check isCommandOnFlight, continue executing, and one of the two sets it to true, the other will still be running...
// solution: use a mutex (but then how to check if the mutex is locked without blocking?) or find how to use channels correctly

func NewSelfDriving() *SelfDriving {
	return &SelfDriving{startStop: make(chan string), initialized: false}
}

func (s *SelfDriving) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Println("Starting controller")

	if !s.initialized {
		s.initialized = true
		s.run()
	}
	s.startStop <- Start

	return nil
}

func (s *SelfDriving) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Println("Stopping controller")

	s.startStop <- Stop

	return nil
}

func (s *SelfDriving) run() {
	go func() {
		run := false
		for {
			select {
			case command, ok := <-s.startStop:
				if ok {
					if command == Start {
						run = true
					} else {
						run = false
					}
				} else {
					fmt.Println("Channel closed!")
					break
				}
			default:
				fmt.Println("No value ready, moving on.")
			}

			if run {
				// My logic here
				fmt.Println("debug: loop")
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

func (s *SelfDriving) Reconcile() {
	// Get current cpu diff
	// Get target per node
	// Check which nodes have diff > target + (5%) or diff < target - (5%)
	//
}
