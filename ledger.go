package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func recordLedgerTransactionTx(
	ctx context.Context,
	tx pgx.Tx,
	paymentID string,
	debtorAccount string,
	creditorAccount string,
	amount string,
	currency string,
) error {

	var ledgerTransactionID string

	err := tx.QueryRow(ctx, `
		INSERT INTO ledger_transactions (
			payment_id,
			currency
		)
		VALUES ($1::uuid, $2)
		RETURNING id
	`,
		paymentID,
		currency,
	).Scan(&ledgerTransactionID)

	if err != nil {
		return fmt.Errorf(
			"failed to create ledger transaction: %w",
			err,
		)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (
			ledger_transaction_id,
			account_id,
			entry_type,
			amount
		)
		VALUES
			($1::uuid, $2, 'debit', $3::numeric),
			($1::uuid, $4, 'credit', $3::numeric)
	`,
		ledgerTransactionID,
		debtorAccount,
		amount,
		creditorAccount,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to create ledger entries: %w",
			err,
		)
	}

	return nil
}
