package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ReconciliationCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type ReconciliationResponse struct {
	PaymentID            string                `json:"payment_id"`
	PaymentStatus        string                `json:"payment_status"`
	ReconciliationStatus string                `json:"reconciliation_status"`
	Amount               string                `json:"amount"`
	Currency             string                `json:"currency"`
	LedgerTransactions   int64                 `json:"ledger_transaction_count"`
	LedgerEntries        int64                 `json:"ledger_entry_count"`
	DebitTotal           string                `json:"debit_total"`
	CreditTotal          string                `json:"credit_total"`
	Checks               []ReconciliationCheck `json:"checks"`
}

func handleReconciliation(
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

	paymentID :=
		r.URL.Path[len("/reconciliation/"):]

	if paymentID == "" {
		http.Error(
			w,
			"payment id is required",
			http.StatusBadRequest,
		)
		return
	}

	result, err :=
		reconcilePayment(
			r.Context(),
			paymentID,
		)

	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "22P02" {

			http.Error(
				w,
				"invalid payment id",
				http.StatusBadRequest,
			)
			return
		}

		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			http.Error(
				w,
				"payment not found",
				http.StatusNotFound,
			)
			return
		}

		http.Error(
			w,
			"reconciliation failed: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	json.NewEncoder(w).Encode(
		result,
	)
}

func reconcilePayment(
	ctx context.Context,
	paymentID string,
) (*ReconciliationResponse, error) {

	/*
		Get the payment record first.

		This is the workflow's version of what
		should have happened.
	*/

	var paymentStatus string
	var paymentAmount string
	var paymentCurrency string
	var debtorAccount string
	var creditorAccount string

	err := db.QueryRow(
		ctx,
		`
		SELECT
			status,
			amount::text,
			currency,
			debtor_account,
			creditor_account
		FROM payments
		WHERE id = $1::uuid
		`,
		paymentID,
	).Scan(
		&paymentStatus,
		&paymentAmount,
		&paymentCurrency,
		&debtorAccount,
		&creditorAccount,
	)

	if err != nil {
		return nil, err
	}

	/*
		Now independently inspect the ledger.

		Notice that PostgreSQL performs the numeric
		comparisons.

		We do NOT convert money into float64 in Go.
	*/

	var ledgerTransactionCount int64
	var ledgerEntryCount int64
	var debitEntryCount int64
	var creditEntryCount int64

	var debitTotal string
	var creditTotal string

	var debitMatchesPayment bool
	var creditMatchesPayment bool
	var debitCreditBalanced bool
	var currencyMatches bool

	var debtorDebitCount int64
	var creditorCreditCount int64

	err = db.QueryRow(
		ctx,
		`
		SELECT
			COUNT(DISTINCT lt.id),

			COUNT(le.id),

			COUNT(le.id)
				FILTER (
					WHERE le.entry_type = 'debit'
				),

			COUNT(le.id)
				FILTER (
					WHERE le.entry_type = 'credit'
				),

			COALESCE(
				SUM(le.amount)
					FILTER (
						WHERE le.entry_type = 'debit'
					),
				0
			)::text,

			COALESCE(
				SUM(le.amount)
					FILTER (
						WHERE le.entry_type = 'credit'
					),
				0
			)::text,

			COALESCE(
				SUM(le.amount)
					FILTER (
						WHERE le.entry_type = 'debit'
					),
				0
			) = $2::numeric,

			COALESCE(
				SUM(le.amount)
					FILTER (
						WHERE le.entry_type = 'credit'
					),
				0
			) = $2::numeric,

			COALESCE(
				SUM(le.amount)
					FILTER (
						WHERE le.entry_type = 'debit'
					),
				0
			)
			=
			COALESCE(
				SUM(le.amount)
					FILTER (
						WHERE le.entry_type = 'credit'
					),
				0
			),

			(
				COUNT(DISTINCT lt.currency) = 1
				AND
				MIN(lt.currency) = $3
			),

			COUNT(le.id)
				FILTER (
					WHERE
						le.entry_type = 'debit'
						AND le.account_id = $4
				),

			COUNT(le.id)
				FILTER (
					WHERE
						le.entry_type = 'credit'
						AND le.account_id = $5
				)

		FROM ledger_transactions lt

		LEFT JOIN ledger_entries le
			ON le.ledger_transaction_id = lt.id

		WHERE lt.payment_id = $1::uuid
		`,
		paymentID,
		paymentAmount,
		paymentCurrency,
		debtorAccount,
		creditorAccount,
	).Scan(
		&ledgerTransactionCount,
		&ledgerEntryCount,
		&debitEntryCount,
		&creditEntryCount,
		&debitTotal,
		&creditTotal,
		&debitMatchesPayment,
		&creditMatchesPayment,
		&debitCreditBalanced,
		&currencyMatches,
		&debtorDebitCount,
		&creditorCreditCount,
	)

	if err != nil {
		return nil, err
	}

	result :=
		&ReconciliationResponse{
			PaymentID:          paymentID,
			PaymentStatus:      paymentStatus,
			Amount:             paymentAmount,
			Currency:           paymentCurrency,
			LedgerTransactions: ledgerTransactionCount,
			LedgerEntries:      ledgerEntryCount,
			DebitTotal:         debitTotal,
			CreditTotal:        creditTotal,
			Checks:             []ReconciliationCheck{},
		}

	/*
		Terminal successful payment.

		A settled payment MUST have exactly
		one correct financial settlement.
	*/

	if paymentStatus == "settled" {

		result.Checks =
			append(
				result.Checks,

				ReconciliationCheck{
					Name: "single_ledger_transaction",

					Passed: ledgerTransactionCount == 1,

					Detail: fmt.Sprintf(
						"expected 1 ledger transaction, found %d",
						ledgerTransactionCount,
					),
				},

				ReconciliationCheck{
					Name: "exactly_two_ledger_entries",

					Passed: ledgerEntryCount == 2,

					Detail: fmt.Sprintf(
						"expected 2 ledger entries, found %d",
						ledgerEntryCount,
					),
				},

				ReconciliationCheck{
					Name: "single_debit_entry",

					Passed: debitEntryCount == 1,

					Detail: fmt.Sprintf(
						"expected 1 debit entry, found %d",
						debitEntryCount,
					),
				},

				ReconciliationCheck{
					Name: "single_credit_entry",

					Passed: creditEntryCount == 1,

					Detail: fmt.Sprintf(
						"expected 1 credit entry, found %d",
						creditEntryCount,
					),
				},

				ReconciliationCheck{
					Name: "debit_matches_payment",

					Passed: debitMatchesPayment,

					Detail: fmt.Sprintf(
						"payment amount %s, debit total %s",
						paymentAmount,
						debitTotal,
					),
				},

				ReconciliationCheck{
					Name: "credit_matches_payment",

					Passed: creditMatchesPayment,

					Detail: fmt.Sprintf(
						"payment amount %s, credit total %s",
						paymentAmount,
						creditTotal,
					),
				},

				ReconciliationCheck{
					Name: "debits_equal_credits",

					Passed: debitCreditBalanced,

					Detail: fmt.Sprintf(
						"debit total %s, credit total %s",
						debitTotal,
						creditTotal,
					),
				},

				ReconciliationCheck{
					Name: "currency_matches",

					Passed: currencyMatches,

					Detail: fmt.Sprintf(
						"expected currency %s",
						paymentCurrency,
					),
				},

				ReconciliationCheck{
					Name: "debtor_was_debited",

					Passed: debtorDebitCount == 1,

					Detail: fmt.Sprintf(
						"expected debtor account %s to have one debit",
						debtorAccount,
					),
				},

				ReconciliationCheck{
					Name: "creditor_was_credited",

					Passed: creditorCreditCount == 1,

					Detail: fmt.Sprintf(
						"expected creditor account %s to have one credit",
						creditorAccount,
					),
				},
			)

		result.ReconciliationStatus =
			"pass"

		for _, check := range result.Checks {

			if !check.Passed {
				result.ReconciliationStatus =
					"fail"

				break
			}
		}

		return result, nil
	}

	/*
		A failed or rejected payment must NOT
		have moved money.

		Therefore there should be no ledger
		transaction and no ledger entries.
	*/

	if paymentStatus == "failed" ||
		paymentStatus == "rejected" {

		noLedgerTransaction :=
			ledgerTransactionCount == 0

		noLedgerEntries :=
			ledgerEntryCount == 0

		result.Checks =
			append(
				result.Checks,

				ReconciliationCheck{
					Name: "no_ledger_transaction",

					Passed: noLedgerTransaction,

					Detail: fmt.Sprintf(
						"expected 0 ledger transactions, found %d",
						ledgerTransactionCount,
					),
				},

				ReconciliationCheck{
					Name: "no_ledger_entries",

					Passed: noLedgerEntries,

					Detail: fmt.Sprintf(
						"expected 0 ledger entries, found %d",
						ledgerEntryCount,
					),
				},
			)

		if noLedgerTransaction &&
			noLedgerEntries {

			result.ReconciliationStatus =
				"pass"

		} else {

			result.ReconciliationStatus =
				"fail"
		}

		return result, nil
	}

	/*
		The payment is still processing.

		It is too early to decide whether the
		final financial records reconcile.
	*/

	result.ReconciliationStatus =
		"pending"

	result.Checks =
		append(
			result.Checks,
			ReconciliationCheck{
				Name: "payment_terminal",

				Passed: false,

				Detail: "payment is still processing; reconciliation will be meaningful after it reaches a terminal state",
			},
		)

	return result, nil
}
