package middleware

import (
	"net/http"
	"runtime/debug"

	"speech-server/internal/logger"
)

// Recovery recovers from panics in the wrapped handler and logs the stack.
func Recovery(next http.Handler) http.Handler {
	if next == nil {
		next = http.DefaultServeMux
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Errorf("panic recovered: %v\n%s", rec, debug.Stack())
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
