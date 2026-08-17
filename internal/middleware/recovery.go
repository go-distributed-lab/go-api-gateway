package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"
)

// Recovery returns a middleware that catches panics in downstream handlers,
// logs the stack trace, and returns a 500 Internal Server Error.
// Without this, a panic kills the serving goroutine and the client hangs.
func Recovery(onPanic func(err any, stack []byte)) Func {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stack := debug.Stack()
					if onPanic != nil {
						onPanic(err, stack)
					}
					// Only write the header if nothing has been sent yet.
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					w.Header().Set("X-Content-Type-Options", "nosniff")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = fmt.Fprintf(w, "internal server error\n")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
