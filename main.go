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
	"log"
	"net/http"
	"time"
	"os"

	"github.com/joho/godotenv"
	"github.com/jackc/pgx/v5/pgxpool"
)

// global database connection pool (keeping reusable database connections instead of opening new 
// database connection for every request)
var db *pgxpool.Pool

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

	http.HandleFunc("/payments", handlePayment)
	http.HandleFunc("/uetr/", handleGetPaymentByUETR) // API endpoint: GET http://localhost:8080/uetr/{uetr}

	log.Println("server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil)) // API endpoint: POST http://localhost:8080/payments
}

func handlePayment(w http.ResponseWriter, r *http.Request) {
	// validation to allow only POST
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var doc Document // creating empty Document struct
	// reading JSON of request body and filling Document 
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest) // 404 Bad Request (Client mistake)
		return
	}

	tx := doc.FIToFICstmrCdtTrf.CdtTrfTxInf // tx is payment transaction object
	// assume validated status as default
	status := "validated"
	rejectReason := ""

	if err := validatePayment(doc); err != nil {
		status = "rejected"
		rejectReason = err.Error() // rejected payment recorded for future debugging
	}

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
	err := db.QueryRow(context.Background(), query,
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
		status,
		rejectReason,
	).Scan(&id, &createdAt)

	if err != nil {
		http.Error(w, "database error: "+err.Error(), http.StatusConflict) // 409 Conflict
		return
	}

	resp := map[string]string{
		"id":            id,
		"uetr":          tx.PmtId.UETR,
		"end_to_end_id": tx.PmtId.EndToEndId,
		"status":        status,
		"reject_reason": rejectReason,
		"created_at":    createdAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if status == "rejected" {
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