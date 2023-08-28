package scheduling

type Strategy interface {
	Reconcile()
	Start()
	Stop()
}

type ConcurrentStrategy interface {
	Strategy
	Run()
}

type BaseConcurrentStrategy struct {
	reconcile func()
}

func (c BaseConcurrentStrategy) Reconcile() {
	c.reconcile()
}

func (c BaseConcurrentStrategy) Start() {

}

func (c BaseConcurrentStrategy) Stop() {
	//TODO implement me
	panic("implement me")
}

func (c BaseConcurrentStrategy) Run() {
	//TODO implement me
	panic("implement me")
}
