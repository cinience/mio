package runtime

import (
	"fmt"
	"log"
	"runtime/debug"
)

// protectServiceCall wraps a service lifecycle function and converts panics into
// errors so the aio runtime can log them and continue supervising the process.
func protectServiceCall(name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s recovered from panic: %v", name, r)
			log.Printf("[aio-runtime] %s panic recovered: %v\n%s", name, r, debug.Stack())
		}
	}()
	return fn()
}
