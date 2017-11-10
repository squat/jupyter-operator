package retry

import (
	"fmt"
	"time"
)

func Example() {
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
}
