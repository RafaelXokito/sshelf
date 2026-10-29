//go:build unix

package term

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func resizeEvents() (<-chan struct{}, func()) {
	sig := make(chan os.Signal, 1)
	out := make(chan struct{}, 1)
	done := make(chan struct{})
	signal.Notify(sig, syscall.SIGWINCH)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-sig:
				select {
				case out <- struct{}{}:
				default: // an event is already pending
				}
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return out, func() {
		once.Do(func() {
			signal.Stop(sig)
			close(done)
			wg.Wait()
		})
	}
}
