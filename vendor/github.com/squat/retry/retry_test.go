package retry

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var _ BackOff = ConstantBackOff{}

func TestStopped(t *testing.T) {
	done := make(chan struct{})
	if stopped(done) {
		t.Error("expected chan not to be stopped")
	}
	close(done)
	if !stopped(done) {
		t.Error("expected chan to be stopped")
	}
}

func TestSleepOrDone(t *testing.T) {
	start := time.Now()
	wait := 10 * time.Second

	done := make(chan struct{})
	close(done)

	sleepOrDone(wait, done)
	elapsed := time.Since(start)

	if elapsed >= wait {
		t.Error("expected to return immediately")
	}
}

func TestRetry(t *testing.T) {
	errorFunc := func() error { return errors.New("foo") }
	type testCase struct {
		f        Retriable
		b        BackOff
		done     chan struct{}
		err      error
		finished bool
	}
	d1 := make(chan struct{})
	close(d1)
	testCases := []testCase{
		{
			f:        errorFunc,
			b:        ConstantBackOff{10 * time.Second},
			done:     d1,
			err:      errTimedOut,
			finished: true,
		},
		{
			f:        errorFunc,
			b:        ConstantBackOff{time.Duration(-1)},
			done:     make(chan struct{}),
			err:      errMaxRetries,
			finished: true,
		},
		{
			f:        func() error { return nil },
			b:        ConstantBackOff{time.Duration(10 * time.Second)},
			done:     make(chan struct{}),
			err:      nil,
			finished: true,
		},
		{
			f:        errorFunc,
			b:        ConstantBackOff{time.Duration(10 * time.Second)},
			done:     make(chan struct{}),
			err:      fmt.Errorf("got %v; retrying in %v", errorFunc(), time.Duration(10*time.Second)),
			finished: false,
		},
	}

	for i, tc := range testCases {
		mc := Retry(tc.b, tc.done, tc.f)
		m := <-mc
		if m.Error != tc.err && m.Error.Error() != tc.err.Error() {
			t.Errorf("test case %d: expected error to be %v but got %v", i, tc.err, m.Error)
		}
		if m.Done != tc.finished {
			t.Errorf("test case %d: expected message 'done' field to be %v but got %v", i, tc.finished, m.Done)
		}
		if m.Done && !stopped(tc.done) {
			t.Errorf("test case %d: expected done chan to be closed", i)
		}
	}
}
