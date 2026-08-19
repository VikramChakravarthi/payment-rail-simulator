// Flow: POST /payment
// check if method is POST
// decode JSON into Document
// extract CdtTrfTxInf as tx
// validate paymen fields
// set status as validated or rejected
// insert into Postgress
// return JSON response
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// global database connection pool (keeping reusable database connections instead of opening new
// database connection for every request)
var db *pgxpool.Pool

type TransitionLogEntry struct {
	SequenceNumber int64  `json:"sequence_number"`
	FromState      string `json:"from_state"`
	ToState        string `json:"to_state"`
	EventType      string `json:"event_type"`
	Reason         string `json:"reason"`
	CreatedAt      string `json:"created_at"`
}

type ClearPaymentResponse struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	DebtorAccount   string `json:"debtor_account"`
	CreditorAccount string `json:"creditor_account"`
	DebtorAgent     string `json:"debtor_agent"`
	CreditorAgent   string `json:"creditor_agent"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	Message         string `json:"message"`
}

func main() {
	var err error
	// Load .env file first
	if err := godotenv.Load(); err != nil {
		log.Println("Note: No .env file found")
	}

	// connecting to postgres using
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err = pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatal("unable to connect to database", err)
	}

	// close database pool when program shuts down
	defer db.Close()

	rateLimiter := NewRateLimiter(5, 10) // 5 requests per second, burst of 10

	http.HandleFunc("/payments", CORSMiddleware(LoggingMiddleware(rateLimiter.MiddleWare(handlePayment))))

	http.HandleFunc("/payments/clear", CORSMiddleware(LoggingMiddleware(rateLimiter.MiddleWare(handleClearPayment))))

	http.HandleFunc("/transition-log", CORSMiddleware(LoggingMiddleware(rateLimiter.MiddleWare(handleGetTransitionLog)))) // API endpoint: GET http://localhost:8080/transition-log?payment_id={payment_id}

	http.HandleFunc("/uetr/", CORSMiddleware(LoggingMiddleware(rateLimiter.MiddleWare(handleGetPaymentByUETR)))) // API endpoint: GET http://localhost:8080/uetr/{uetr}

	log.Println("server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil)) // API endpoint: POST http://localhost:8080/payments
}

func handlePayment(w http.ResponseWriter, r *http.Request) {
	// validation to allow only POST
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context() // context of the request, used for database operations and cancellation

	var doc Document // creating empty Document struct
	// reading JSON of request body and filling Document
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest) // 404 Bad Request (Client mistake)
		return
	}

	tx := doc.FIToFICstmrCdtTrf.CdtTrfTxInf // tx is payment transaction object
	event := EventValidationPassed
	rejectReason := ""

	if err := validatePayment(doc); err != nil {
		event = EventValidationFailed
		rejectReason = err.Error() // rejected payment recorded for future debugging
	}

	dbTx, err := db.Begin(ctx) // start database transaction
	if err != nil {
		http.Error(w, "falied to start database transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	defer dbTx.Rollback(ctx) // rollback transaction if function exits before commit

	var id string
	var createdAt time.Time

	// SQL querry to insert payment into the payments table
	query := `
		INSERT INTO payments
		(uetr, end_to_end_id, instr_id, tx_id, msg_id, amount, currency,
		 debtor_name, debtor_account, debtor_agent,
		 creditor_name, creditor_account, creditor_agent,
		 remittance_info, status, reject_reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id, created_at`

	// Go sends payments values, Postgress inserts row, Postgress generates id and created_at,
	// postgress returns them, Go stores them in id and createdAt, Go includes them in JSON response
	err = dbTx.QueryRow(ctx, query,
		tx.PmtId.UETR,
		tx.PmtId.EndToEndId,
		tx.PmtId.InstrId,
		tx.PmtId.TxId,
		doc.FIToFICstmrCdtTrf.GrpHdr.MsgId,
		tx.IntrBkSttlmAmt.Value,
		tx.IntrBkSttlmAmt.Ccy,
		tx.Dbtr.Nm,
		tx.DbtrAcct.Id.Othr.Id,
		tx.DbtrAgt.FinInstnId.ClrSysMmbId.MmbId,
		tx.Cdtr.Nm,
		tx.CdtrAcct.Id.Othr.Id,
		tx.CdtrAgt.FinInstnId.ClrSysMmbId.MmbId,
		tx.RmtInf.Ustrd,
		string(StateReceived),
		"",
	).Scan(&id, &createdAt)

	if err != nil {
		var pgErr *pgconn.PgError                            // error type assertion to check if error is a Postgres error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // 23505 is Postgres error code for unique violation
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict) // 409 Conflict
			json.NewEncoder(w).Encode(map[string]string{
				"error": "payment with the same UETR already exists",
				"uetr":  tx.PmtId.UETR,
			})
			return
		}
		http.Error(w, "database error: "+err.Error(), http.StatusConflict) // 409 Conflict
		return
	}

	finalState, err := applyPaymentEventTx(ctx, dbTx, id, event, rejectReason)
	if err != nil {
		http.Error(w, "state transition error: "+err.Error(), http.StatusInternalServerError) // 500 Internal Server Error
		return
	}

	if err := dbTx.Commit(ctx); err != nil {
		http.Error(w, "failed to commit transaction: "+err.Error(), http.StatusInternalServerError) // 500 Internal Server Error
		return
	}

	resp := map[string]string{
		"id":            id,
		"uetr":          tx.PmtId.UETR,
		"end_to_end_id": tx.PmtId.EndToEndId,
		"status":        string(finalState),
		"reject_reason": rejectReason,
		"created_at":    createdAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if finalState == StateRejected {
		w.WriteHeader(http.StatusUnprocessableEntity) // 422 Unprocessable Entity
	} else {
		w.WriteHeader(http.StatusCreated) // 201 Created
	}
	json.NewEncoder(w).Encode(resp)
}

func handleGetPaymentByUETR(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// extract UETR from the URL path
	uetr := r.URL.Path[len("/uetr/"):]

	if uetr == "" {
		http.Error(w, "uetr is required", http.StatusBadRequest)
		return
	}

	var id, EndToEndId, status, rejectReason, currency string
	var createdAt time.Time
	var amount float64

	query := `
		SELECT id, uetr, end_to_end_id, status, reject_reason, created_at,
		amount, currency
		FROM payments
		WHERE uetr = $1`

	err := db.QueryRow(context.Background(), query, uetr).Scan(
		&id, &uetr, &EndToEndId, &status, &rejectReason, &createdAt, &amount, &currency,
	)

	if err != nil {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"id":            id,
		"uetr":          uetr,
		"end_to_end_id": EndToEndId,
		"status":        status,
		"reject_reason": rejectReason,
		"created_at":    createdAt.Format(time.RFC3339),
		"amount":        amount,
		"currency":      currency,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

}

func handleGetTransitionLog(w http.ResponseWriter, r *http.Request) {
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

	query := `
		SELECT sequence_number, from_state, to_state, event_type, reason, created_at
		FROM payment_transition_log
		WHERE payment_id = $1
		ORDER BY sequence_number ASC`

	rows, err := db.Query(ctx, query, paymentID)
	if err != nil {
		http.Error(w, "failed to query transition log: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close() // close rows when function exits

	var transitionLogs []TransitionLogEntry

	for rows.Next() {
		var entry TransitionLogEntry
		var createdAt time.Time

		err := rows.Scan(
			&entry.SequenceNumber,
			&entry.FromState,
			&entry.ToState,
			&entry.EventType,
			&entry.Reason,
			&createdAt,
		)

		if err != nil {
			http.Error(w, "failed to scan transition log: "+err.Error(), http.StatusInternalServerError)
			return
		}

		entry.CreatedAt = createdAt.Format(time.RFC3339)
		transitionLogs = append(transitionLogs, entry)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "error iterating over transition log rows: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if len(transitionLogs) == 0 {
		http.Error(w, "transition log not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transitionLogs)
}

func handleClearPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	paymentID := r.URL.Query().Get("payment_id")
	if paymentID == "" {
		http.Error(w, "payment_id is required", http.StatusBadRequest)
		return
	}

	dbTx, err := db.Begin(ctx)
	if err != nil {
		http.Error(w, "failed to start database transaction: "+err.Error(), http.StatusInternalServerError)
		return
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
		SELECT status, debtor_account, creditor_account, debtor_agent, creditor_agent, amount::text, currency
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
	if err != nil {
		http.Error(w, "payment not found: "+err.Error(), http.StatusNotFound)
		return
	}

	if currentState != StateValidated {
		http.Error(w, "payment must be validated before clearing", http.StatusConflict)
		return
	}

	_, err = applyPaymentEventTx(ctx, dbTx, paymentID, EventClearingStarted, "")
	if err != nil {
		http.Error(w, "state transition error: failed to start clearing: "+err.Error(), http.StatusInternalServerError)
		return
	}

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
	`, creditorAccount, creditorAgent, currency).Scan(&creditorExists)
	if err != nil {
		http.Error(w, "failed to check creditor account: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !creditorExists {
		finalState, transitionErr := applyPaymentEventTx(ctx, dbTx, paymentID, EventClearingFailed, "creditor account does not exist or is inactive")
		if transitionErr != nil {
			http.Error(w, "failed to record clearing failure: "+transitionErr.Error(), http.StatusInternalServerError)
			return
		}

		if err := dbTx.Commit(ctx); err != nil {
			http.Error(w, "failed to commit clearing failure: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(ClearPaymentResponse{
			ID:              paymentID,
			Status:          string(finalState),
			DebtorAccount:   debtorAccount,
			CreditorAccount: creditorAccount,
			DebtorAgent:     debtorAgent,
			CreditorAgent:   creditorAgent,
			Amount:          amount,
			Currency:        currency,
			Message:         "creditor account does not exist or is inactive",
		})
		return
	}

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
	`, amount, debtorAccount, debtorAgent, currency).Scan(&reservedAccountID)

	if err != nil {
		finalState, transitionErr := applyPaymentEventTx(ctx, dbTx, paymentID, EventClearingFailed, "debtor account has insufficient available funds or is inactive")
		if transitionErr != nil {
			http.Error(w, "failed to record clearing failure: "+transitionErr.Error(), http.StatusInternalServerError)
			return
		}

		if err := dbTx.Commit(ctx); err != nil {
			http.Error(w, "failed to commit clearing failure: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(ClearPaymentResponse{
			ID:              paymentID,
			Status:          string(finalState),
			DebtorAccount:   debtorAccount,
			CreditorAccount: creditorAccount,
			DebtorAgent:     debtorAgent,
			CreditorAgent:   creditorAgent,
			Amount:          amount,
			Currency:        currency,
			Message:         "debtor account has insufficient available funds or is inactive",
		})
		return
	}

	finalState, err := applyPaymentEventTx(ctx, dbTx, paymentID, EventClearingPassed, "")
	if err != nil {
		http.Error(w, "failed to complete clearing: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := dbTx.Commit(ctx); err != nil {
		http.Error(w, "failed to commit clearing: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ClearPaymentResponse{
		ID:              paymentID,
		Status:          string(finalState),
		DebtorAccount:   debtorAccount,
		CreditorAccount: creditorAccount,
		DebtorAgent:     debtorAgent,
		CreditorAgent:   creditorAgent,
		Amount:          amount,
		Currency:        currency,
		Message:         "payment cleared and forwarded to the receiver bank",
	})
}
