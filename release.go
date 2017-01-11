package gps

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// This file contains SourceMgr teardown logic. It's kept separately to increase
// the readability of source_manager.go.

// releaser is a helper for a *SourceMgr that handles all teardown-related
// logic.
type releaser struct {
	sm        *SourceMgr    // the SourceMgr being managed
	qch       chan struct{} // quit chan for signal handler
	sigmut    sync.Mutex    // mutex protecting signal handling setup/teardown
	releasing int32         // flag indicating release of sm has begun
	relonce   sync.Once     // once-er to ensure we only release once
	lf        *os.File      // handle for the sm lock file on disk
}

// Release lets go of any locks held by the SourceManager. Once called, it is no
// longer safe to call methods against it; all method calls will immediately
// result in errors.
func (r *releaser) Release() {
	// Set sm.releasing before entering the Once func to guarantee that no
	// _more_ method calls will stack up if/while waiting.
	atomic.CompareAndSwapInt32(&r.releasing, 0, 1)

	// Whether 'releasing' is set or not, we don't want this function to return
	// until after the doRelease process is done, as doing so could cause the
	// process to terminate before a signal-driven doRelease() call has a chance
	// to finish its cleanup.
	r.relonce.Do(func() { r.doRelease() })
}

// doRelease actually releases physical resources (files on disk, etc.).
//
// This must be called only and exactly once. Calls to it should be wrapped in
// the sm.relonce sync.Once instance.
func (r *releaser) doRelease() {
	// Grab the global sm lock so that we only release once we're sure all other
	// calls have completed
	r.sm.glock.Lock()

	// Close the file handle for the lock file
	r.lf.Close()
	// Remove the lock file from disk
	os.Remove(filepath.Join(r.sm.cachedir, "sm.lock"))
	// Close the qch, if non-nil, so the signal handlers run out. This will
	// also deregister the sig channel, if any has been set up.
	if r.qch != nil {
		close(r.qch)
	}
	r.sm.glock.Unlock()
}

// UseDefaultSignalHandling sets up typical os.Interrupt signal handling for a
// SourceMgr.
func (r *releaser) UseDefaultSignalHandling() {
	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt)
	r.HandleSignals(sigch)
}

// HandleSignals sets up logic to handle incoming signals with the goal of
// shutting down the SourceMgr safely.
//
// Calling code must provide the signal channel, and is responsible for calling
// signal.Notify() on that channel.
//
// Successive calls to HandleSignals() will deregister the previous handler and
// set up a new one. It is not recommended that the same channel be passed
// multiple times to this method.
//
// SetUpSigHandling() will set up a handler that is appropriate for most
// use cases.
func (r *releaser) HandleSignals(sigch chan os.Signal) {
	r.sigmut.Lock()
	// always start by closing the qch, which will lead to any existing signal
	// handler terminating, and deregistering its sigch.
	if r.qch != nil {
		close(r.qch)
	}
	r.qch = make(chan struct{})

	// Run a new goroutine with the input sigch and the fresh qch
	go func(sch chan os.Signal, qch <-chan struct{}) {
		defer signal.Stop(sch)
		for {
			select {
			case <-sch:
				// Set up a timer to uninstall the signal handler after three
				// seconds, so that the user can easily force termination with a
				// second ctrl-c
				go func(c <-chan time.Time) {
					<-c
					signal.Stop(sch)
				}(time.After(3 * time.Second))

				if !atomic.CompareAndSwapInt32(&r.releasing, 0, 1) {
					// Something's already called Release() on this sm, so we
					// don't have to do anything, as we'd just be redoing
					// that work. Instead, deregister and return.
					return
				}

				opc := atomic.LoadInt32(&r.sm.opcount)
				if opc > 0 {
					fmt.Printf("Signal received: waiting for %v ops to complete...\n", opc)
				}

				// Mutex interaction in a signal handler is, as a general rule,
				// unsafe. I'm not clear on whether the guarantees Go provides
				// around signal handling, or having passed this through a
				// channel in general, obviate those concerns, but it's a lot
				// easier to just rely on the mutex contained in the Once right
				// now, so do that until it proves problematic or someone
				// provides a clear explanation.
				r.relonce.Do(func() { r.doRelease() })
				return
			case <-qch:
				// quit channel triggered - deregister our sigch and return
				return
			}
		}
	}(sigch, r.qch)
	// Try to ensure handler is blocked in for-select before releasing the mutex
	runtime.Gosched()

	r.sigmut.Unlock()
}

// StopSignalHandling deregisters any signal handler running on this SourceMgr.
//
// It's normally not necessary to call this directly; it will be called as
// needed by Release().
func (r *releaser) StopSignalHandling() {
	r.sigmut.Lock()
	if r.qch != nil {
		close(r.qch)
		r.qch = nil
		runtime.Gosched()
	}
	r.sigmut.Unlock()
}

func (r *releaser) isReleased() bool {
	return atomic.CompareAndSwapInt32(&r.releasing, 1, 1)
}
