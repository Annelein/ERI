package runtimer

import (
	"os"
	"os/signal"
)

type Callback func(s os.Signal)

func New(signals ...os.Signal) *SignalHandler {
	c := make(chan os.Signal, 1)
	signal.Notify(c, signals...)

	sh := &SignalHandler{
		c: c,
	}

	go sh.handle()

	return sh
}

type SignalHandler struct {
	c   chan os.Signal
	fns []Callback
}

func (sh *SignalHandler) handle() {
	s := <-sh.c
	signal.Stop(sh.c)
	close(sh.c)

	for _, fn := range sh.fns {
		fn(s)
	}
}

func (sh *SignalHandler) RegisterCallback(fn Callback) {
	sh.fns = append(sh.fns, fn)
}
