package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type LedgerEntryResponse struct {
	AccountID string `json:"account_id"`
	EntryType string `json:"entry_type"`
	Amount    string `json:"amount"`
	CreatedAt string `json:"created_at"`
}

type LedgerResponse struct {
	PaymentID           string                `json:"payment_id"`
	LedgerTransactionID string                `json:"ledger_transaction_id"`
	Currency            string                `json:"currency"`
	CreatedAt           string                `json:"created_at"`
	Entries             []LedgerEntryResponse `json:"entries"`
}

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

func handleGetLedger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	paymentID := r.URL.Query().Get("payment_id")

	if paymentID == "" {
		http.Error(w, "payment_id is required", http.StatusBadRequest)
		return
	}

	var ledgerTransactionID string
	var currency string
	var createdAt time.Time

	err := db.QueryRow(ctx, `
		SELECT
			id,
			currency,
			created_at
		FROM ledger_transactions
		WHERE payment_id = $1::uuid
	`,
		paymentID,
	).Scan(
		&ledgerTransactionID,
		&currency,
		&createdAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			http.Error(w, "ledger transaction not found", http.StatusNotFound)
			return
		}

		http.Error(
			w,
			"failed to query ledger transaction: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	rows, err := db.Query(ctx, `
		SELECT
			account_id,
			entry_type,
			amount::text,
			created_at
		FROM ledger_entries
		WHERE ledger_transaction_id = $1::uuid
		ORDER BY id ASC
	`,
		ledgerTransactionID,
	)

	if err != nil {
		http.Error(
			w,
			"failed to query ledger entries: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	defer rows.Close()

	entries := []LedgerEntryResponse{}

	for rows.Next() {
		var entry LedgerEntryResponse
		var entryCreatedAt time.Time

		err := rows.Scan(
			&entry.AccountID,
			&entry.EntryType,
			&entry.Amount,
			&entryCreatedAt,
		)

		if err != nil {
			http.Error(
				w,
				"failed to scan ledger entry: "+err.Error(),
				http.StatusInternalServerError,
			)
			return
		}

		entry.CreatedAt = entryCreatedAt.Format(time.RFC3339)

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		http.Error(
			w,
			"error iterating over ledger entries: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	resp := LedgerResponse{
		PaymentID:           paymentID,
		LedgerTransactionID: ledgerTransactionID,
		Currency:            currency,
		CreatedAt:           createdAt.Format(time.RFC3339),
		Entries:             entries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
