package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var ErrPaymentNotAwaitingResponse = errors.New(
	"payment must be awaiting receiver response",
)

type ReceiverPaymentResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func respondToPayment(
	ctx context.Context,
	paymentID string,
	accepted bool,
	reason string,
) (ReceiverPaymentResult, error) {

	dbTx, err := db.Begin(ctx)
	if err != nil {
		return ReceiverPaymentResult{}, fmt.Errorf(
			"failed to start receiver transaction: %w",
			err,
		)
	}

	defer dbTx.Rollback(ctx)

	var currentState PaymentState
	var debtorAccount string
	var debtorAgent string
	var amount string
	var currency string

	err = dbTx.QueryRow(ctx, `
		SELECT
			status,
			debtor_account,
			debtor_agent,
			amount::text,
			currency
		FROM payments
		WHERE id = $1::uuid
		FOR UPDATE
	`, paymentID).Scan(
		&currentState,
		&debtorAccount,
		&debtorAgent,
		&amount,
		&currency,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return ReceiverPaymentResult{}, ErrPaymentNotFound
	}

	if err != nil {
		return ReceiverPaymentResult{}, fmt.Errorf(
			"failed to read payment: %w",
			err,
		)
	}

	if currentState != StateAwaitingResponse {
		return ReceiverPaymentResult{}, ErrPaymentNotAwaitingResponse
	}

	// ACCEPTED
	if accepted {
		finalState, err := applyPaymentEventTx(
			ctx,
			dbTx,
			paymentID,
			EventReceiverAccepted,
			"",
		)

		if err != nil {
			return ReceiverPaymentResult{}, fmt.Errorf(
				"failed to accept payment: %w",
				err,
			)
		}

		err = recordPaymentAcceptedOutboxTx(
			ctx,
			dbTx,
			paymentID,
		)

		if err != nil {
			return ReceiverPaymentResult{}, fmt.Errorf(
				"failed to record payment accepted event: %w",
				err,
			)
		}

		if err := dbTx.Commit(ctx); err != nil {
			return ReceiverPaymentResult{}, fmt.Errorf(
				"failed to commit receiver acceptance: %w",
				err,
			)
		}

		return ReceiverPaymentResult{
			ID:      paymentID,
			Status:  string(finalState),
			Message: "receiver accepted payment",
		}, nil
	}

	// REJECTED
	if reason == "" {
		reason = "receiver rejected payment"
	}

	finalState, err := applyPaymentEventTx(
		ctx,
		dbTx,
		paymentID,
		EventReceiverRejected,
		reason,
	)

	if err != nil {
		return ReceiverPaymentResult{}, fmt.Errorf(
			"failed to reject payment: %w",
			err,
		)
	}

	// a rejected payment must release the money
	// that clearing previously reserved
	var releasedAccountID string

	err = dbTx.QueryRow(ctx, `
		UPDATE accounts
		SET reserved_balance = reserved_balance - $1::numeric
		WHERE account_id = $2
		  AND routing_number = $3
		  AND currency = $4
		  AND reserved_balance >= $1::numeric
		RETURNING account_id
	`,
		amount,
		debtorAccount,
		debtorAgent,
		currency,
	).Scan(&releasedAccountID)

	if err != nil {
		return ReceiverPaymentResult{}, fmt.Errorf(
			"failed to release reserved funds: %w",
			err,
		)
	}

	if err := dbTx.Commit(ctx); err != nil {
		return ReceiverPaymentResult{}, fmt.Errorf(
			"failed to commit receiver rejection: %w",
			err,
		)
	}

	return ReceiverPaymentResult{
		ID:      paymentID,
		Status:  string(finalState),
		Message: reason,
	}, nil
}
