package retry

import (
	"errors"
	"fmt"
	"time"
)

const (
	stopBackOff = time.Duration(-1)
)

var (
	errTimedOut   = errors.New("timed out waiting for operation to finish")
	errMaxRetries = errors.New("the operation reached the maximum amount of retries")
)

// Retriable is a function that encapsulates an operation that should
// be retried upon returning an error.
type Retriable func() error

// MessageHandler is a function that accepts and handles a Message from a Retrier
// for logging or flow control purposes.
type MessageHandler func(Message)

// ErrorHandler is a function that accepts and handles an error from a Retrier
// for logging or flow control purposes.
type ErrorHandler func(error)

// BackOff describes a basic interface for a type that generates
// new backoff times. See https://godoc.org/github.com/cenkalti/backoff#ExponentialBackOff
// for an example implementation.
type BackOff interface {
	NextBackOff() time.Duration
}

// ConstantBackOff is a basic implementation of the BackOff interface that always returns
// the same backoff.
type ConstantBackOff struct {
	// BackOff is the duration that will be returned by NextBackOff.
	BackOff time.Duration
}

// NextBackOff implements the BackOff interface.
func (cb ConstantBackOff) NextBackOff() time.Duration {
	return cb.BackOff
}
func stopped(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func sleepOrDone(sleep time.Duration, done <-chan struct{}) {
	select {
	case <-time.After(sleep):
		return
	case <-done:
		return
	}
}

// Message is a type that encapsulates messages sent from a retrier.
type Message struct {
	// Done is a boolean that states whether or not the operation is done being retried.
	Done bool
	// Error is the error returned from retrying the operation.
	Error error
}

// Retrier is a convenience type that retries a given Retriable function.
// It exposes additional methods that facilitate interacting with chans.
type Retrier struct {
	f        func() error
	b        BackOff
	done     chan struct{}
	messages <-chan Message
}

// NewRetrier returns a new Retrier for the given Retriable and BackOff.
func NewRetrier(f func() error, b BackOff) Retrier {
	var r Retrier
	r.f = f
	r.done = make(chan struct{})
	messages := Retry(b, r.done, f)
	r.messages = messages
	return r
}

// Stop stops the Retrier. Stop can safely be called more than once.
func (r Retrier) Stop() {
	if !stopped(r.done) {
		close(r.done)
	}
	return
}

// Notify registers a MessageHandler, which handles messages from the Retrier.
func (r Retrier) Notify(messageHandler MessageHandler) {
	go func() {
		for !stopped(r.done) {
			m := <-r.messages
			if messageHandler != nil {
				messageHandler(m)
			}
		}
	}()
}

// Done will block until the Retriable has finished or errored permanently.
func (r Retrier) Done() {
	<-r.done
	return
}

// Retry will continue to retry a Retriable until it is stopped via the done chan or
// the BackOff returns -1.
func Retry(b BackOff, done chan struct{}, f Retriable) <-chan Message {
	messages := make(chan Message)

	go func() {
		defer close(messages)
		var err error
		var next time.Duration

		for !stopped(done) {
			if err = f(); err == nil {
				sendNonblockingMessage(Message{true, nil}, messages)
				close(done)
				return
			}
			if next = b.NextBackOff(); next == stopBackOff {
				sendNonblockingMessage(Message{true, errMaxRetries}, messages)
				close(done)
				return
			}
			sendNonblockingMessage(Message{false, fmt.Errorf("got %v; retrying in %v", err, next)}, messages)
			sleepOrDone(next, done)
		}
		sendNonblockingMessage(Message{true, errTimedOut}, messages)
	}()

	return messages
}

func sendNonblockingMessage(m Message, c chan<- Message) {
	select {
	case c <- m:
	default:
	}
	return
}

// Timeout is a cancellable timer that returns a chan that returns when the timer is done.
// The timer can be cancelled early by closing the returned chan.
func Timeout(d time.Duration) chan struct{} {
	t := time.NewTimer(d)
	c := make(chan struct{})

	go func() {
		select {
		case <-t.C:
			close(c)
			return
		case <-c:
			t.Stop()
			return
		}
	}()

	return c
}
