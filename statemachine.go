package main

import "fmt"

type PaymentState string

const (
	StateReceived         PaymentState = "received"
	StateValidated        PaymentState = "validated"
	StateRejected         PaymentState = "rejected"
	StateClearing         PaymentState = "clearing"
	StateAwaitingResponse PaymentState = "awaiting_response"
	StateFailed           PaymentState = "failed"
)

type PaymentEvent string

const (
	EventValidationPassed PaymentEvent = "validation_passed"
	EventValidationFailed PaymentEvent = "validation_failed"

	EventClearingStarted PaymentEvent = "clearing_started"
	EventClearingPassed  PaymentEvent = "clearing_passed"
	EventClearingFailed  PaymentEvent = "clearing_failed"
)

func NextPaymentState(current PaymentState, event PaymentEvent) (PaymentState, error) {
	switch current {
	case StateReceived:
		switch event {
		case EventValidationPassed:
			return StateValidated, nil
		case EventValidationFailed:
			return StateRejected, nil
		default:
			return "", fmt.Errorf("invalid event %s for state %s", event, current)
		}

	case StateValidated:
		switch event {
		case EventClearingStarted:
			return StateClearing, nil
		default:
			return "", fmt.Errorf("invalid event %s for state %s", event, current)
		}
	case StateClearing:
		switch event {
		case EventClearingPassed:
			return StateAwaitingResponse, nil
		case EventClearingFailed:
			return StateFailed, nil
		default:
			return "", fmt.Errorf("invalid event %s for state %s", event, current)
		}

	default:
		return "", fmt.Errorf("invalid state %s for event %s", current, event)
	}
}
