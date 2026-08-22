package main

import "testing"

func TestNextPaymentStateValidTransitions(t *testing.T) {
	tests := []struct {
		name     string
		current  PaymentState
		event    PaymentEvent
		expected PaymentState
	}{
		{
			name:     "validation passes",
			current:  StateReceived,
			event:    EventValidationPassed,
			expected: StateValidated,
		},
		{
			name:     "validation fails",
			current:  StateReceived,
			event:    EventValidationFailed,
			expected: StateRejected,
		},
		{
			name:     "clearing starts",
			current:  StateValidated,
			event:    EventClearingStarted,
			expected: StateClearing,
		},
		{
			name:     "clearing passes",
			current:  StateClearing,
			event:    EventClearingPassed,
			expected: StateAwaitingResponse,
		},
		{
			name:     "clearing fails",
			current:  StateClearing,
			event:    EventClearingFailed,
			expected: StateFailed,
		},
		{
			name:     "receiver accepts",
			current:  StateAwaitingResponse,
			event:    EventReceiverAccepted,
			expected: StateAccepted,
		},
		{
			name:     "receiver rejects",
			current:  StateAwaitingResponse,
			event:    EventReceiverRejected,
			expected: StateFailed,
		},
		{
			name:     "settlement starts",
			current:  StateAccepted,
			event:    EventSettlementStarted,
			expected: StateSettling,
		},
		{
			name:     "settlement completes",
			current:  StateSettling,
			event:    EventSettlementCompleted,
			expected: StateSettled,
		},
		{
			name:     "settlement fails",
			current:  StateSettling,
			event:    EventSettlementFailed,
			expected: StateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := NextPaymentState(tt.current, tt.event)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if actual != tt.expected {
				t.Fatalf(
					"expected state %s, got %s",
					tt.expected,
					actual,
				)
			}
		})
	}
}

func TestNextPaymentStateInvalidTransitions(t *testing.T) {
	tests := []struct {
		name    string
		current PaymentState
		event   PaymentEvent
	}{
		{
			name:    "cannot settle received payment",
			current: StateReceived,
			event:   EventSettlementStarted,
		},
		{
			name:    "cannot accept before clearing",
			current: StateValidated,
			event:   EventReceiverAccepted,
		},
		{
			name:    "cannot clear settled payment",
			current: StateSettled,
			event:   EventClearingStarted,
		},
		{
			name:    "cannot settle twice",
			current: StateSettled,
			event:   EventSettlementCompleted,
		},
		{
			name:    "cannot accept failed payment",
			current: StateFailed,
			event:   EventReceiverAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NextPaymentState(tt.current, tt.event)

			if err == nil {
				t.Fatalf(
					"expected transition from %s using %s to fail",
					tt.current,
					tt.event,
				)
			}
		})
	}
}
