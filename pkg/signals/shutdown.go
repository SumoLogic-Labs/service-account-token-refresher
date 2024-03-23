package signals

import (
	"os"
	"os/signal"
	"syscall"
)

// onlyOneHandler ensures at most 1 shutdown handler is registered
var onlyOneHandler = make(chan struct{})

// SignalShutdown returns a stop channel which is closed on receiving an interrupt signal,
// giving the application a chance to shutdown gracefully. After the first signal,
// it forwards any further signals directly to the application causing it to force-quit.
func SignalShutdown() <-chan struct{} {
	close(onlyOneHandler) // panics when called more than once
	stopCh := make(chan struct{})
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		signal.Reset()
		close(stopCh)
	}()
	return stopCh
}
