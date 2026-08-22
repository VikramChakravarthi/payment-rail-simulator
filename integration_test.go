package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// TestMain runs once before all tests.
// It connects the global db variable to fednow_test instead of fednow.
func TestMain(m *testing.M) {
	_ = godotenv.Load()

	testDatabaseURL := os.Getenv("TEST_DATABASE_URL")

	// If TEST_DATABASE_URL is not explicitly set,
	// derive fednow_test from DATABASE_URL.
	if testDatabaseURL == "" {
		databaseURL := os.Getenv("DATABASE_URL")

		if databaseURL == "" {
			fmt.Println("DATABASE_URL is required for integration tests")
			os.Exit(1)
		}

		parsedURL, err := url.Parse(databaseURL)
		if err != nil {
			fmt.Println("failed to parse DATABASE_URL:", err)
			os.Exit(1)
		}

		parsedURL.Path = "/fednow_test"
		testDatabaseURL = parsedURL.String()
	}

	var err error

	db, err = pgxpool.New(context.Background(), testDatabaseURL)
	if err != nil {
		fmt.Println("failed to connect to test database:", err)
		os.Exit(1)
	}

	if err := db.Ping(context.Background()); err != nil {
		fmt.Println("failed to ping test database:", err)
		os.Exit(1)
	}

	code := m.Run()

	db.Close()

	os.Exit(code)
}

// resetTestDB resets the database before each integration test.
//
// This is important because every test must start from exactly
// the same balances and with no existing payments/ledger records.
func resetTestDB(t *testing.T) {
	t.Helper()

	ctx := context.Background()

	// Ledger tables must also be cleared now because they reference payments.
	_, err := db.Exec(ctx, `
		TRUNCATE
			outbox_events,
			ledger_entries,
			ledger_transactions,
			payment_transition_log,
			payments
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("failed to clear test database: %v", err)
	}

	// Reset the two accounts used by our tests.
	_, err = db.Exec(ctx, `
		UPDATE accounts
		SET
			balance = CASE
				WHEN account_id = '12345678901234567890123456789012'
					THEN 10000.00
				WHEN account_id = '23456789012345678901234567890123'
					THEN 200000.00
				ELSE balance
			END,
			reserved_balance = 0.00,
			status = 'active'
		WHERE account_id IN (
			'12345678901234567890123456789012',
			'23456789012345678901234567890123'
		)
	`)
	if err != nil {
		t.Fatalf("failed to reset test accounts: %v", err)
	}
}

// makePaymentJSON creates a valid payment request.
// We change the UETR and amount depending on the test.
func makePaymentJSON(uetr string, amount float64) string {
	return fmt.Sprintf(`
	{
		"FIToFICstmrCdtTrf": {
			"GrpHdr": {
				"MsgId": "TEST-MSG-001",
				"CreDtTm": "2026-08-20T10:00:00Z",
				"NbOfTxs": "1",
				"SttlmInf": {
					"SttlmMtd": "CLRG"
				}
			},
			"CdtTrfTxInf": {
				"PmtId": {
					"InstrId": "TEST-INSTR-001",
					"EndToEndId": "TEST-E2E-001",
					"TxId": "TEST-TX-001",
					"UETR": "%s"
				},
				"IntrBkSttlmAmt": {
					"Ccy": "USD",
					"value": %.2f
				},
				"ChrgBr": "SLEV",
				"Dbtr": {
					"Nm": "John Doe"
				},
				"DbtrAcct": {
					"Id": {
						"Othr": {
							"Id": "12345678901234567890123456789012"
						}
					}
				},
				"DbtrAgt": {
					"FinInstnId": {
						"ClrSysMmbId": {
							"MmbId": "987654321"
						}
					}
				},
				"CdtrAgt": {
					"FinInstnId": {
						"ClrSysMmbId": {
							"MmbId": "876543210"
						}
					}
				},
				"Cdtr": {
					"Nm": "Jane Smith"
				},
				"CdtrAcct": {
					"Id": {
						"Othr": {
							"Id": "23456789012345678901234567890123"
						}
					}
				},
				"RmtInf": {
					"Ustrd": "automated integration test"
				}
			}
		}
	}`, uetr, amount)
}

// performRequest lets the tests call our HTTP handlers without
// actually starting the server on localhost:8080.
func performRequest(
	handler http.HandlerFunc,
	method string,
	target string,
	body string,
) *httptest.ResponseRecorder {

	requestBody := strings.NewReader(body)

	req := httptest.NewRequest(
		method,
		target,
		requestBody,
	)

	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler(recorder, req)

	return recorder
}

// createTestPayment creates a payment and returns its generated database ID.
func createTestPayment(
	t *testing.T,
	uetr string,
	amount float64,
) string {

	t.Helper()

	response := performRequest(
		handlePayment,
		http.MethodPost,
		"/payments",
		makePaymentJSON(uetr, amount),
	)

	if response.Code != http.StatusCreated {
		t.Fatalf(
			"expected create payment status 201, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}

	if err := json.Unmarshal(
		response.Body.Bytes(),
		&result,
	); err != nil {
		t.Fatalf(
			"failed to decode create payment response: %v",
			err,
		)
	}

	if result.Status != string(StateValidated) {
		t.Fatalf(
			"expected payment state validated, got %s",
			result.Status,
		)
	}

	return result.ID
}

// getAccountBalances reads the current balance and reservation
// directly from PostgreSQL.
func getAccountBalances(
	t *testing.T,
	accountID string,
) (balance string, reservedBalance string) {

	t.Helper()

	err := db.QueryRow(
		context.Background(),
		`
		SELECT
			balance::text,
			reserved_balance::text
		FROM accounts
		WHERE account_id = $1
		`,
		accountID,
	).Scan(
		&balance,
		&reservedBalance,
	)

	if err != nil {
		t.Fatalf(
			"failed to query account balance: %v",
			err,
		)
	}

	return balance, reservedBalance
}

// getPaymentStatus reads the state directly from the payments table.
func getPaymentStatus(
	t *testing.T,
	paymentID string,
) string {

	t.Helper()

	var status string

	err := db.QueryRow(
		context.Background(),
		`
		SELECT status
		FROM payments
		WHERE id = $1::uuid
		`,
		paymentID,
	).Scan(&status)

	if err != nil {
		t.Fatalf(
			"failed to query payment status: %v",
			err,
		)
	}

	return status
}

// LedgerEntry represents one ledger row returned from PostgreSQL.
type LedgerEntry struct {
	AccountID string
	EntryType string
	Amount    string
}

// getLedgerTransactionCount returns the number of ledger transactions
// associated with one payment.
func getLedgerTransactionCount(
	t *testing.T,
	paymentID string,
) int {

	t.Helper()

	var count int

	err := db.QueryRow(
		context.Background(),
		`
		SELECT COUNT(*)
		FROM ledger_transactions
		WHERE payment_id = $1::uuid
		`,
		paymentID,
	).Scan(&count)

	if err != nil {
		t.Fatalf(
			"failed to count ledger transactions: %v",
			err,
		)
	}

	return count
}

// getLedgerEntries retrieves all financial entries for one payment.
func getLedgerEntries(
	t *testing.T,
	paymentID string,
) []LedgerEntry {

	t.Helper()

	rows, err := db.Query(
		context.Background(),
		`
		SELECT
			le.account_id,
			le.entry_type,
			le.amount::text
		FROM ledger_entries le
		JOIN ledger_transactions lt
			ON le.ledger_transaction_id = lt.id
		WHERE lt.payment_id = $1::uuid
		ORDER BY le.id
		`,
		paymentID,
	)

	if err != nil {
		t.Fatalf(
			"failed to query ledger entries: %v",
			err,
		)
	}

	defer rows.Close()

	var entries []LedgerEntry

	for rows.Next() {
		var entry LedgerEntry

		if err := rows.Scan(
			&entry.AccountID,
			&entry.EntryType,
			&entry.Amount,
		); err != nil {
			t.Fatalf(
				"failed to scan ledger entry: %v",
				err,
			)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf(
			"failed while reading ledger entries: %v",
			err,
		)
	}

	return entries
}

// assertLedgerBalanced verifies:
//
// total debit amount == total credit amount
func assertLedgerBalanced(
	t *testing.T,
	paymentID string,
) {

	t.Helper()

	var debitTotal string
	var creditTotal string

	err := db.QueryRow(
		context.Background(),
		`
		SELECT
			COALESCE(
				SUM(amount) FILTER (
					WHERE entry_type = 'debit'
				),
				0
			)::text,
			COALESCE(
				SUM(amount) FILTER (
					WHERE entry_type = 'credit'
				),
				0
			)::text
		FROM ledger_entries
		WHERE ledger_transaction_id = (
			SELECT id
			FROM ledger_transactions
			WHERE payment_id = $1::uuid
		)
		`,
		paymentID,
	).Scan(
		&debitTotal,
		&creditTotal,
	)

	if err != nil {
		t.Fatalf(
			"failed to calculate ledger totals: %v",
			err,
		)
	}

	if debitTotal != creditTotal {
		t.Fatalf(
			"ledger is unbalanced: debits=%s credits=%s",
			debitTotal,
			creditTotal,
		)
	}
}

// ------------------------------------------------------------
// TEST 1
// Successful payment from beginning through settlement.
// ------------------------------------------------------------

func TestSuccessfulPaymentFlow(t *testing.T) {
	resetTestDB(t)

	paymentID := createTestPayment(
		t,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		300.00,
	)

	// Clearing
	response := performRequest(
		handleClearPayment,
		http.MethodPost,
		"/payments/clear?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected clearing status 200, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	// Clearing should reserve $300 but not debit the account yet.
	balance, reserved := getAccountBalances(
		t,
		"12345678901234567890123456789012",
	)

	if balance != "10000.00" {
		t.Fatalf(
			"expected debtor balance 10000.00, got %s",
			balance,
		)
	}

	if reserved != "300.00" {
		t.Fatalf(
			"expected reserved balance 300.00, got %s",
			reserved,
		)
	}

	// Receiver accepts.
	response = performRequest(
		handleReceiverResponse,
		http.MethodPost,
		"/payments/respond?payment_id="+paymentID,
		`{"accepted": true}`,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected receiver response status 200, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	if status := getPaymentStatus(
		t,
		paymentID,
	); status != string(StateAccepted) {
		t.Fatalf(
			"expected accepted state, got %s",
			status,
		)
	}

	// Settlement.
	response = performRequest(
		handleSettlePayment,
		http.MethodPost,
		"/payments/settle?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected settlement status 200, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	if status := getPaymentStatus(
		t,
		paymentID,
	); status != string(StateSettled) {
		t.Fatalf(
			"expected settled state, got %s",
			status,
		)
	}

	debtorBalance, debtorReserved := getAccountBalances(
		t,
		"12345678901234567890123456789012",
	)

	creditorBalance, creditorReserved := getAccountBalances(
		t,
		"23456789012345678901234567890123",
	)

	if debtorBalance != "9700.00" {
		t.Fatalf(
			"expected debtor balance 9700.00, got %s",
			debtorBalance,
		)
	}

	if debtorReserved != "0.00" {
		t.Fatalf(
			"expected debtor reserved balance 0.00, got %s",
			debtorReserved,
		)
	}

	if creditorBalance != "200300.00" {
		t.Fatalf(
			"expected creditor balance 200300.00, got %s",
			creditorBalance,
		)
	}

	if creditorReserved != "0.00" {
		t.Fatalf(
			"expected creditor reserved balance 0.00, got %s",
			creditorReserved,
		)
	}
}

// ------------------------------------------------------------
// TEST 2
// Receiver rejection releases reserved funds.
// ------------------------------------------------------------

func TestReceiverRejectionReleasesReservation(t *testing.T) {
	resetTestDB(t)

	paymentID := createTestPayment(
		t,
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		400.00,
	)

	response := performRequest(
		handleClearPayment,
		http.MethodPost,
		"/payments/clear?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected clearing status 200, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	balance, reserved := getAccountBalances(
		t,
		"12345678901234567890123456789012",
	)

	if balance != "10000.00" {
		t.Fatalf(
			"expected balance 10000.00, got %s",
			balance,
		)
	}

	if reserved != "400.00" {
		t.Fatalf(
			"expected reserved balance 400.00, got %s",
			reserved,
		)
	}

	response = performRequest(
		handleReceiverResponse,
		http.MethodPost,
		"/payments/respond?payment_id="+paymentID,
		`{
			"accepted": false,
			"reason": "beneficiary account blocked"
		}`,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected rejection response 200, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	if status := getPaymentStatus(
		t,
		paymentID,
	); status != string(StateFailed) {
		t.Fatalf(
			"expected failed state, got %s",
			status,
		)
	}

	debtorBalance, debtorReserved := getAccountBalances(
		t,
		"12345678901234567890123456789012",
	)

	creditorBalance, _ := getAccountBalances(
		t,
		"23456789012345678901234567890123",
	)

	if debtorBalance != "10000.00" {
		t.Fatalf(
			"expected debtor balance unchanged at 10000.00, got %s",
			debtorBalance,
		)
	}

	if debtorReserved != "0.00" {
		t.Fatalf(
			"expected reservation released to 0.00, got %s",
			debtorReserved,
		)
	}

	if creditorBalance != "200000.00" {
		t.Fatalf(
			"expected creditor balance unchanged at 200000.00, got %s",
			creditorBalance,
		)
	}

	// Rejected payment must not create financial ledger postings.
	count := getLedgerTransactionCount(
		t,
		paymentID,
	)

	if count != 0 {
		t.Fatalf(
			"expected rejected payment to have no ledger transaction, got %d",
			count,
		)
	}
}

// ------------------------------------------------------------
// TEST 3
// Payment larger than available funds must fail clearing.
// ------------------------------------------------------------

func TestInsufficientFundsFailsClearing(t *testing.T) {
	resetTestDB(t)

	// John has $10,000.
	paymentID := createTestPayment(
		t,
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		20000.00,
	)

	response := performRequest(
		handleClearPayment,
		http.MethodPost,
		"/payments/clear?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"expected clearing status 422, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	if status := getPaymentStatus(
		t,
		paymentID,
	); status != string(StateFailed) {
		t.Fatalf(
			"expected failed state, got %s",
			status,
		)
	}

	debtorBalance, debtorReserved := getAccountBalances(
		t,
		"12345678901234567890123456789012",
	)

	creditorBalance, _ := getAccountBalances(
		t,
		"23456789012345678901234567890123",
	)

	if debtorBalance != "10000.00" {
		t.Fatalf(
			"debtor balance changed unexpectedly: %s",
			debtorBalance,
		)
	}

	if debtorReserved != "0.00" {
		t.Fatalf(
			"expected no reservation, got %s",
			debtorReserved,
		)
	}

	if creditorBalance != "200000.00" {
		t.Fatalf(
			"creditor balance changed unexpectedly: %s",
			creditorBalance,
		)
	}
}

// ------------------------------------------------------------
// TEST 4
// Settling the same payment twice must not move money twice.
// ------------------------------------------------------------

func TestDuplicateSettlementDoesNotMoveMoneyTwice(t *testing.T) {
	resetTestDB(t)

	paymentID := createTestPayment(
		t,
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		300.00,
	)

	response := performRequest(
		handleClearPayment,
		http.MethodPost,
		"/payments/clear?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"clearing failed: %s",
			response.Body.String(),
		)
	}

	response = performRequest(
		handleReceiverResponse,
		http.MethodPost,
		"/payments/respond?payment_id="+paymentID,
		`{"accepted": true}`,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"receiver acceptance failed: %s",
			response.Body.String(),
		)
	}

	// First settlement succeeds.
	response = performRequest(
		handleSettlePayment,
		http.MethodPost,
		"/payments/settle?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"first settlement failed: %s",
			response.Body.String(),
		)
	}

	// Second settlement must fail.
	response = performRequest(
		handleSettlePayment,
		http.MethodPost,
		"/payments/settle?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusConflict {
		t.Fatalf(
			"expected duplicate settlement status 409, got %d: %s",
			response.Code,
			response.Body.String(),
		)
	}

	debtorBalance, debtorReserved := getAccountBalances(
		t,
		"12345678901234567890123456789012",
	)

	creditorBalance, _ := getAccountBalances(
		t,
		"23456789012345678901234567890123",
	)

	if debtorBalance != "9700.00" {
		t.Fatalf(
			"duplicate settlement changed debtor balance: %s",
			debtorBalance,
		)
	}

	if debtorReserved != "0.00" {
		t.Fatalf(
			"unexpected reserved balance after settlement: %s",
			debtorReserved,
		)
	}

	if creditorBalance != "200300.00" {
		t.Fatalf(
			"duplicate settlement changed creditor balance: %s",
			creditorBalance,
		)
	}

	// There must still be only ONE ledger transaction.
	ledgerTransactionCount := getLedgerTransactionCount(
		t,
		paymentID,
	)

	if ledgerTransactionCount != 1 {
		t.Fatalf(
			"expected exactly 1 ledger transaction after duplicate settlement attempt, got %d",
			ledgerTransactionCount,
		)
	}

	// And only TWO entries: one debit and one credit.
	entries := getLedgerEntries(
		t,
		paymentID,
	)

	if len(entries) != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries after duplicate settlement attempt, got %d",
			len(entries),
		)
	}

	assertLedgerBalanced(
		t,
		paymentID,
	)
}

// ------------------------------------------------------------
// TEST 5
// Duplicate UETR must be rejected.
// ------------------------------------------------------------

func TestDuplicateUETRRejected(t *testing.T) {
	resetTestDB(t)

	uetr := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"

	firstResponse := performRequest(
		handlePayment,
		http.MethodPost,
		"/payments",
		makePaymentJSON(
			uetr,
			100.00,
		),
	)

	if firstResponse.Code != http.StatusCreated {
		t.Fatalf(
			"expected first payment 201, got %d: %s",
			firstResponse.Code,
			firstResponse.Body.String(),
		)
	}

	secondResponse := performRequest(
		handlePayment,
		http.MethodPost,
		"/payments",
		makePaymentJSON(
			uetr,
			100.00,
		),
	)

	if secondResponse.Code != http.StatusConflict {
		t.Fatalf(
			"expected duplicate UETR 409, got %d: %s",
			secondResponse.Code,
			secondResponse.Body.String(),
		)
	}
}

// ------------------------------------------------------------
// TEST 6
// Successful settlement must create exactly one balanced
// double-entry ledger transaction.
// ------------------------------------------------------------

func TestSettlementCreatesBalancedLedger(t *testing.T) {
	resetTestDB(t)

	paymentID := createTestPayment(
		t,
		"ffffffff-ffff-4fff-8fff-ffffffffffff",
		300.00,
	)

	response := performRequest(
		handleClearPayment,
		http.MethodPost,
		"/payments/clear?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"clearing failed: %s",
			response.Body.String(),
		)
	}

	response = performRequest(
		handleReceiverResponse,
		http.MethodPost,
		"/payments/respond?payment_id="+paymentID,
		`{"accepted": true}`,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"receiver acceptance failed: %s",
			response.Body.String(),
		)
	}

	response = performRequest(
		handleSettlePayment,
		http.MethodPost,
		"/payments/settle?payment_id="+paymentID,
		"",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"settlement failed: %s",
			response.Body.String(),
		)
	}

	// Exactly one ledger transaction should exist.
	count := getLedgerTransactionCount(
		t,
		paymentID,
	)

	if count != 1 {
		t.Fatalf(
			"expected 1 ledger transaction, got %d",
			count,
		)
	}

	entries := getLedgerEntries(
		t,
		paymentID,
	)

	if len(entries) != 2 {
		t.Fatalf(
			"expected 2 ledger entries, got %d",
			len(entries),
		)
	}

	debit := entries[0]
	credit := entries[1]

	// Debit must belong to John.
	if debit.AccountID != "12345678901234567890123456789012" {
		t.Fatalf(
			"unexpected debit account: %s",
			debit.AccountID,
		)
	}

	if debit.EntryType != "debit" {
		t.Fatalf(
			"expected first entry to be debit, got %s",
			debit.EntryType,
		)
	}

	if debit.Amount != "300.00" {
		t.Fatalf(
			"expected debit amount 300.00, got %s",
			debit.Amount,
		)
	}

	// Credit must belong to Jane.
	if credit.AccountID != "23456789012345678901234567890123" {
		t.Fatalf(
			"unexpected credit account: %s",
			credit.AccountID,
		)
	}

	if credit.EntryType != "credit" {
		t.Fatalf(
			"expected second entry to be credit, got %s",
			credit.EntryType,
		)
	}

	if credit.Amount != "300.00" {
		t.Fatalf(
			"expected credit amount 300.00, got %s",
			credit.Amount,
		)
	}

	// And the total debit must equal the total credit.
	assertLedgerBalanced(
		t,
		paymentID,
	)
}

func TestValidatedPaymentCreatesOutboxEvent(t *testing.T) {
	resetTestDB(t)

	paymentID := createTestPayment(
		t,
		"33333333-cccc-4ccc-8ccc-333333333333",
		300.00,
	)

	var eventType string
	var storedPaymentID string
	var payloadPaymentID string
	var publishedAtIsNull bool

	err := db.QueryRow(
		context.Background(),
		`
		SELECT
			event_type,
			payment_id::text,
			payload->>'payment_id',
			published_at IS NULL
		FROM outbox_events
		WHERE payment_id = $1::uuid
		`,
		paymentID,
	).Scan(
		&eventType,
		&storedPaymentID,
		&payloadPaymentID,
		&publishedAtIsNull,
	)

	if err != nil {
		t.Fatalf(
			"failed to query outbox event: %v",
			err,
		)
	}

	if eventType != "payment.validated" {
		t.Fatalf(
			"expected payment.validated event, got %s",
			eventType,
		)
	}

	if storedPaymentID != paymentID {
		t.Fatalf(
			"expected payment ID %s, got %s",
			paymentID,
			storedPaymentID,
		)
	}

	if payloadPaymentID != paymentID {
		t.Fatalf(
			"expected payload payment ID %s, got %s",
			paymentID,
			payloadPaymentID,
		)
	}

	if !publishedAtIsNull {
		t.Fatal(
			"expected new outbox event to be unpublished",
		)
	}
}

func TestGetNextUnpublishedOutboxEvent(t *testing.T) {
	resetTestDB(t)

	uetr := "44444444-dddd-4ddd-8ddd-444444444444"

	paymentID := createTestPayment(
		t,
		uetr,
		300.00,
	)

	event, err := getNextUnpublishedOutboxEvent(
		context.Background(),
	)

	if err != nil {
		t.Fatalf("failed to get unpublished outbox event: %v", err)
	}

	if event == nil {
		t.Fatal("expected unpublished outbox event, got nil")
	}

	if event.PaymentID != paymentID {
		t.Fatalf(
			"expected payment ID %s, got %s",
			paymentID,
			event.PaymentID,
		)
	}

	if event.EventType != "payment.validated" {
		t.Fatalf(
			"expected event type payment.validated, got %s",
			event.EventType,
		)
	}

	var payload map[string]string

	err = json.Unmarshal(
		[]byte(event.PayLoad),
		&payload,
	)

	if err != nil {
		t.Fatalf(
			"failed to decode outbox payload: %v",
			err,
		)
	}

	if payload["payment_id"] != paymentID {
		t.Fatalf(
			"expected payload payment ID %s, got %s",
			paymentID,
			payload["payment_id"],
		)
	}

	if payload["uetr"] != uetr {
		t.Fatalf(
			"expected UETR %s, got %s",
			uetr,
			payload["uetr"],
		)
	}
}
