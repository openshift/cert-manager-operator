package features

import "fmt"

type TickWatcher struct {
	tickChan chan struct{}
	closed   bool
}

func NewTickWatcher() *TickWatcher {
	return &TickWatcher{
		tickChan: make(chan struct{}),
		closed:   false,
	}
}

func (e *TickWatcher) SendTick() error {
	if e.closed {
		return fmt.Errorf("cannot send tick to closed watch channel")
	}

	e.tickChan <- struct{}{}
	return nil
}

func (e *TickWatcher) IsClosed() bool {
	return e.closed
}

func (e *TickWatcher) Close() {
	e.closed = true
}

func (e *TickWatcher) GetTicker() <-chan struct{} {
	return e.tickChan
}
