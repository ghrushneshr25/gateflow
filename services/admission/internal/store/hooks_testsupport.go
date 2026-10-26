package store

import "sync/atomic"

// Test-only synchronization hooks. Production paths call maybeGrantHook with a
// nil default so behavior is unchanged when hooks are unset.
//
// These exist solely for P6.3 fence-linearization proof tests.

type grantHookFn func(phase string)

type hookBox struct{ fn grantHookFn }

var grantHook atomic.Pointer[hookBox]

// SetGrantHook installs a hook invoked at named phases inside grant transactions.
// Pass nil to clear. Not for production use.
func SetGrantHook(fn grantHookFn) {
	if fn == nil {
		grantHook.Store(nil)
		return
	}
	grantHook.Store(&hookBox{fn: fn})
}

func maybeGrantHook(phase string) {
	b := grantHook.Load()
	if b == nil || b.fn == nil {
		return
	}
	b.fn(phase)
}
