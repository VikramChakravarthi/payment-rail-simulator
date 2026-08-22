package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPaymentNotFound     = errors.New("payment not found")
	ErrPaymentNotValidated = errors.New("payment must be validated before clearing")
)

func clearPayment(
	ctx context.Context,
	paymentID string,
) (ClearPaymentResponse, error) {

	dbTx, err := db.Begin(ctx)
	if err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to start clearing transaction: %w",
			err,
		)
	}

	defer dbTx.Rollback(ctx)

	var currentState PaymentState
	var debtorAccount string
	var creditorAccount string
	var debtorAgent string
	var creditorAgent string
	var amount string
	var currency string

	err = dbTx.QueryRow(ctx, `
		SELECT
			status,
			debtor_account,
			creditor_account,
			debtor_agent,
			creditor_agent,
			amount::text,
			currency
		FROM payments
		WHERE id = $1::uuid
		FOR UPDATE
	`, paymentID).Scan(
		&currentState,
		&debtorAccount,
		&creditorAccount,
		&debtorAgent,
		&creditorAgent,
		&amount,
		&currency,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return ClearPaymentResponse{}, ErrPaymentNotFound
	}

	if err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to read payment: %w",
			err,
		)
	}

	if currentState != StateValidated {
		return ClearPaymentResponse{}, ErrPaymentNotValidated
	}

	_, err = applyPaymentEventTx(
		ctx,
		dbTx,
		paymentID,
		EventClearingStarted,
		"",
	)

	if err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to start clearing: %w",
			err,
		)
	}

	// Check that the receiving account exists and is usable.
	var creditorExists bool

	err = dbTx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM accounts
			WHERE account_id = $1
			  AND routing_number = $2
			  AND status = 'active'
			  AND currency = $3
		)
	`,
		creditorAccount,
		creditorAgent,
		currency,
	).Scan(&creditorExists)

	if err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to check creditor account: %w",
			err,
		)
	}

	if !creditorExists {
		reason := "creditor account does not exist or is inactive"

		finalState, err := applyPaymentEventTx(
			ctx,
			dbTx,
			paymentID,
			EventClearingFailed,
			reason,
		)

		if err != nil {
			return ClearPaymentResponse{}, fmt.Errorf(
				"failed to record clearing failure: %w",
				err,
			)
		}

		if err := dbTx.Commit(ctx); err != nil {
			return ClearPaymentResponse{}, fmt.Errorf(
				"failed to commit clearing failure: %w",
				err,
			)
		}

		return ClearPaymentResponse{
			ID:              paymentID,
			Status:          string(finalState),
			DebtorAccount:   debtorAccount,
			CreditorAccount: creditorAccount,
			DebtorAgent:     debtorAgent,
			CreditorAgent:   creditorAgent,
			Amount:          amount,
			Currency:        currency,
			Message:         reason,
		}, nil
	}

	// Try to reserve the debtor's funds.
	var reservedAccountID string

	err = dbTx.QueryRow(ctx, `
		UPDATE accounts
		SET reserved_balance = reserved_balance + $1::numeric
		WHERE account_id = $2
		  AND routing_number = $3
		  AND status = 'active'
		  AND currency = $4
		  AND balance - reserved_balance >= $1::numeric
		RETURNING account_id
	`,
		amount,
		debtorAccount,
		debtorAgent,
		currency,
	).Scan(&reservedAccountID)

	if errors.Is(err, pgx.ErrNoRows) {
		reason := "debtor account has insufficient available funds or is inactive"

		finalState, transitionErr := applyPaymentEventTx(
			ctx,
			dbTx,
			paymentID,
			EventClearingFailed,
			reason,
		)

		if transitionErr != nil {
			return ClearPaymentResponse{}, fmt.Errorf(
				"failed to record clearing failure: %w",
				transitionErr,
			)
		}

		if err := dbTx.Commit(ctx); err != nil {
			return ClearPaymentResponse{}, fmt.Errorf(
				"failed to commit clearing failure: %w",
				err,
			)
		}

		return ClearPaymentResponse{
			ID:              paymentID,
			Status:          string(finalState),
			DebtorAccount:   debtorAccount,
			CreditorAccount: creditorAccount,
			DebtorAgent:     debtorAgent,
			CreditorAgent:   creditorAgent,
			Amount:          amount,
			Currency:        currency,
			Message:         reason,
		}, nil
	}

	if err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to reserve debtor funds: %w",
			err,
		)
	}

	finalState, err := applyPaymentEventTx(
		ctx,
		dbTx,
		paymentID,
		EventClearingPassed,
		"",
	)

	if err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to complete clearing: %w",
			err,
		)
	}

	err = recordPaymentClearedOutboxTx(
		ctx,
		dbTx,
		paymentID,
	)

	if err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to record payment cleared event: %w",
			err,
		)
	}

	if err := dbTx.Commit(ctx); err != nil {
		return ClearPaymentResponse{}, fmt.Errorf(
			"failed to commit clearing: %w",
			err,
		)
	}

	return ClearPaymentResponse{
		ID:              paymentID,
		Status:          string(finalState),
		DebtorAccount:   debtorAccount,
		CreditorAccount: creditorAccount,
		DebtorAgent:     debtorAgent,
		CreditorAgent:   creditorAgent,
		Amount:          amount,
		Currency:        currency,
		Message:         "payment cleared and forwarded to the receiver bank",
	}, nil
}
