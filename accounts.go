package main

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type AccountResponse struct {
	AccountID        string `json:"account_id"`
	RoutingNumber    string `json:"routing_number"`
	OwnerName        string `json:"owner_name"`
	Balance          string `json:"balance"`
	ReservedBalance  string `json:"reserved_balance"`
	AvailableBalance string `json:"available_balance"`
	Currency         string `json:"currency"`
	Status           string `json:"status"`
}

func handleGetAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID := r.URL.Path[len("/accounts/"):]

	if accountID == "" {
		http.Error(w, "account_id is required", http.StatusBadRequest)
		return
	}

	var account AccountResponse

	err := db.QueryRow(r.Context(), `
		SELECT
			account_id,
			routing_number,
			owner_name,
			balance::text,
			reserved_balance::text,
			(balance - reserved_balance)::text,
			currency,
			status
		FROM accounts
		WHERE account_id = $1
	`,
		accountID,
	).Scan(
		&account.AccountID,
		&account.RoutingNumber,
		&account.OwnerName,
		&account.Balance,
		&account.ReservedBalance,
		&account.AvailableBalance,
		&account.Currency,
		&account.Status,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}

		http.Error(
			w,
			"failed to query account: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(account)
}
