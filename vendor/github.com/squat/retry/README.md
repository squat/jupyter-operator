# Retry

[![Build Status](https://travis-ci.org/squat/retry.svg?branch=master)](https://travis-ci.org/squat/retry)
[![Go Report Card](https://goreportcard.com/badge/github.com/squat/retry)](https://goreportcard.com/report/github.com/squat/retry)
[![GoDoc](https://godoc.org/github.com/squat/retry?status.png)](https://godoc.org/github.com/squat/retry)

## Usage
```go
b := ConstantBackOff{5 * time.Second}
operation := func() error {
	// ... do some work
	return nil
}
r := NewRetrier(operation, b)
r.Notify(func(m Message) {
	if m.Error != nil {
		fmt.Println(m.Error)
	}
})
// Wait until the operation is done.
r.Done()
fmt.Println("All done!")
```
