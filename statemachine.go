package main

import "fmt"

type PaymentState string

const (
	StateReceived  PaymentState = "received"
	StateValidated PaymentState = "validated"
	StateRejected  PaymentState = "rejected"
)

type PaymentEvent string

const (
	EventValidationPassed PaymentEvent = "validation_passed"
	EventValidationFailed PaymentEvent = "validation_failed"
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
	default:
		return "", fmt.Errorf("invalid state %s for event %s", current, event)
	}
}
