package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var ErrPaymentNotAccepted = errors.New(
	"payment must be accepted before settlement",
)

type SettlementResult struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Message  string `json:"message"`
}

func settlePayment(
	ctx context.Context,
	paymentID string,
) (SettlementResult, error) {

	dbTx, err := db.Begin(ctx)
	if err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to start settlement transaction: %w",
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
		return SettlementResult{}, ErrPaymentNotFound
	}

	if err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to read payment: %w",
			err,
		)
	}

	if currentState != StateAccepted {
		return SettlementResult{}, ErrPaymentNotAccepted
	}

	// accepted → settling
	_, err = applyPaymentEventTx(
		ctx,
		dbTx,
		paymentID,
		EventSettlementStarted,
		"",
	)

	if err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to start settlement: %w",
			err,
		)
	}

	// record the money movement in the ledger 
	err = recordLedgerTransactionTx(
		ctx,
		dbTx,
		paymentID,
		debtorAccount,
		creditorAccount,
		amount,
		currency,
	)

	if err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to record ledger transaction: %w",
			err,
		)
	}

	// debit debtor and release the reserved amount 
	var debitedAccountID string

	err = dbTx.QueryRow(ctx, `
		UPDATE accounts
		SET
			balance = balance - $1::numeric,
			reserved_balance = reserved_balance - $1::numeric
		WHERE account_id = $2
		  AND routing_number = $3
		  AND currency = $4
		  AND status = 'active'
		  AND reserved_balance >= $1::numeric
		  AND balance >= $1::numeric
		RETURNING account_id
	`,
		amount,
		debtorAccount,
		debtorAgent,
		currency,
	).Scan(&debitedAccountID)

	if err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to debit debtor account: %w",
			err,
		)
	}

	// credit receiver 
	var creditedAccountID string

	err = dbTx.QueryRow(ctx, `
		UPDATE accounts
		SET balance = balance + $1::numeric
		WHERE account_id = $2
		  AND routing_number = $3
		  AND currency = $4
		  AND status = 'active'
		RETURNING account_id
	`,
		amount,
		creditorAccount,
		creditorAgent,
		currency,
	).Scan(&creditedAccountID)

	if err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to credit creditor account: %w",
			err,
		)
	}

	// settling → settled
	finalState, err := applyPaymentEventTx(
		ctx,
		dbTx,
		paymentID,
		EventSettlementCompleted,
		"",
	)

	if err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to complete settlement: %w",
			err,
		)
	}

	if err := dbTx.Commit(ctx); err != nil {
		return SettlementResult{}, fmt.Errorf(
			"failed to commit settlement: %w",
			err,
		)
	}

	return SettlementResult{
		ID:       paymentID,
		Status:   string(finalState),
		Amount:   amount,
		Currency: currency,
		Message:  "payment settled successfully",
	}, nil
}