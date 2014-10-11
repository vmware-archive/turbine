package scheduler

import "time"

type Clock interface {
	CurrentTime() time.Time
}

func NewClock() Clock {
	return realClock{}
}

type realClock struct{}

func (realClock) CurrentTime() time.Time {
	return time.Now()
}
