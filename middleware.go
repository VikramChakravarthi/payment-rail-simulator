package main

import (
	"log"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	// this RateLimiter object stores a pointer to an actual rate limiter from golang.org/x/time/rate
	limiter *rate.Limiter
}

// this is a constructor function
// it creates a new RateLimiter
// first input is requestsPerSecond, and its type is rate.Limit
// second input it burst and type is int
// returns a pointer to a RateLimiter
func NewRateLimiter(requestsPerSecond rate.Limit, burst int) *RateLimiter {
	// creates RateLimiter object and returns its address
	return &RateLimiter{
		// setting the limiter field of the struct
		limiter: rate.NewLimiter(requestsPerSecond, burst),
	}

	// above code creates a RateLimiter object. Put rate.NewLimiter(..., ...) inside its limiter field
	// return the address of that object
	// colon : is used because you are filling in a struct field by name
}

// wrapper around another HTTP function
// before running the real endpoint, check if the request is allowed
// if allowed, continue. if not allowed return 429 rate limit exceeded
func (rl *RateLimiter) MiddleWare(next http.HandlerFunc) http.HandlerFunc {
	// this is a method called MiddleWare. it belongs to RateLimiter.
	// it takes one HTTP handler called next. it returns another HTTP handler
	// rl is like self in Python. rl.limiter is like self.limiter
	// next is the function that should run after the middleware check passes

	// below means return a new function.
	// the function returned will run later when an HTTP request comes in
	// this function must return an http.HandlerFunc. a http.HandlerFunc is of the
	// simply func(writer http.ResponseWriter, request *http.Request)
	// so the return in this code handles that
	return func(writer http.ResponseWriter, request *http.Request) {
		// return a function that will run whenever an HTTP request comes in
		// writer is used to send a response back to client
		// request contains information about the incoming request like URL, method, client address, body, headers, etc
		if !rl.limiter.Allow() {
			log.Printf("Rate limiter exceeded for %s", request.RemoteAddr)
			http.Error(writer, "Rate limit exceeded", http.StatusTooManyRequests)
			// because of this return, the real handler does not run
			return
		}

		// if the limiter allows the request, the if block is skipped
		next(writer, request) // next is the real handler
	}
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (recorder *statusRecorder) WriteHeader(
	statusCode int,
) {
	recorder.statusCode =
		statusCode

	recorder.ResponseWriter.WriteHeader(
		statusCode,
	)
}

func (recorder *statusRecorder) Write(
	body []byte,
) (int, error) {

	if recorder.statusCode == 0 {
		recorder.statusCode =
			http.StatusOK
	}

	return recorder.ResponseWriter.Write(
		body,
	)
}

func LoggingMiddleware(
	next http.HandlerFunc,
) http.HandlerFunc {

	return func(
		writer http.ResponseWriter,
		request *http.Request,
	) {

		start :=
			time.Now()

		recorder :=
			&statusRecorder{
				ResponseWriter: writer,

				statusCode: http.StatusOK,
			}

		next(
			recorder,
			request,
		)

		log.Printf(
			"http_request method=%s path=%s status=%d duration=%s remote_addr=%s",
			request.Method,
			request.URL.Path,
			recorder.statusCode,
			time.Since(start),
			request.RemoteAddr,
		)
	}
}

func CORSMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if request.Method == "OPTIONS" {
			writer.WriteHeader(http.StatusOK)
			return
		}

		next(writer, request)
	}
}
