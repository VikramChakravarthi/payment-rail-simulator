package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

/*
	kafkaClient is package-level so the readiness
	endpoint can check the same Kafka client used
	by the outbox publisher.

	main.go will initialize it.
*/

var kafkaClient *kgo.Client

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type DependencyHealth struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type ReadinessResponse struct {
	Status     string                      `json:"status"`
	Timestamp  string                      `json:"timestamp"`
	Components map[string]DependencyHealth `json:"components"`
}

func handleHealth(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	response :=
		HealthResponse{
			Status: "ok",

			Timestamp: time.Now().
				UTC().
				Format(time.RFC3339),
		}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	json.NewEncoder(w).Encode(
		response,
	)
}

func handleReady(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	/*
		Do not allow dependency checks
		to hang forever.
	*/

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			2*time.Second,
		)

	defer cancel()

	response :=
		ReadinessResponse{
			Status: "ready",

			Timestamp: time.Now().
				UTC().
				Format(time.RFC3339),

			Components: map[string]DependencyHealth{},
		}

	statusCode :=
		http.StatusOK

	/*
		POSTGRES
	*/

	if err := db.Ping(ctx); err != nil {

		response.Status =
			"not_ready"

		statusCode =
			http.StatusServiceUnavailable

		response.Components["postgres"] =
			DependencyHealth{
				Status: "down",

				Error: err.Error(),
			}

	} else {

		response.Components["postgres"] =
			DependencyHealth{
				Status: "up",
			}
	}

	/*
		KAFKA
	*/

	if kafkaClient == nil {

		response.Status =
			"not_ready"

		statusCode =
			http.StatusServiceUnavailable

		response.Components["kafka"] =
			DependencyHealth{
				Status: "down",

				Error: "Kafka client is not initialized",
			}

	} else {

		if err :=
			kafkaClient.Ping(ctx); err != nil {

			response.Status =
				"not_ready"

			statusCode =
				http.StatusServiceUnavailable

			response.Components["kafka"] =
				DependencyHealth{
					Status: "down",

					Error: err.Error(),
				}

		} else {

			response.Components["kafka"] =
				DependencyHealth{
					Status: "up",
				}
		}
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(
		statusCode,
	)

	json.NewEncoder(w).Encode(
		response,
	)
}
