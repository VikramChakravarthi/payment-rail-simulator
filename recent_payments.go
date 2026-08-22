package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type RecentPaymentResponse struct {
	ID           string `json:"id"`
	UETR         string `json:"uetr"`
	EndToEndID   string `json:"end_to_end_id"`
	DebtorName   string `json:"debtor_name"`
	CreditorName string `json:"creditor_name"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	Status       string `json:"status"`
	RejectReason string `json:"reject_reason"`
	CreatedAt    string `json:"created_at"`
}

func handleGetRecentPayments(w http.ResponseWriter, r *http.Request) {
	limit := 20

	limitParam := r.URL.Query().Get("limit")

	if limitParam != "" {
		parsedLimit, err := strconv.Atoi(limitParam)

		if err != nil || parsedLimit < 1 {
			http.Error(
				w,
				"limit must be a positive integer",
				http.StatusBadRequest,
			)
			return
		}

		// Do not allow somebody to request an
		// arbitrarily huge result set.
		if parsedLimit > 100 {
			parsedLimit = 100
		}

		limit = parsedLimit
	}

	rows, err := db.Query(
		r.Context(),
		`
		SELECT
			id,
			uetr,
			end_to_end_id,
			debtor_name,
			creditor_name,
			amount::text,
			currency,
			status,
			COALESCE(reject_reason, ''),
			created_at
		FROM payments
		ORDER BY created_at DESC
		LIMIT $1
		`,
		limit,
	)

	if err != nil {
		http.Error(
			w,
			"failed to query recent payments: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	defer rows.Close()

	payments := []RecentPaymentResponse{}

	for rows.Next() {
		var payment RecentPaymentResponse
		var createdAt time.Time

		err := rows.Scan(
			&payment.ID,
			&payment.UETR,
			&payment.EndToEndID,
			&payment.DebtorName,
			&payment.CreditorName,
			&payment.Amount,
			&payment.Currency,
			&payment.Status,
			&payment.RejectReason,
			&createdAt,
		)

		if err != nil {
			http.Error(
				w,
				"failed to scan payment: "+err.Error(),
				http.StatusInternalServerError,
			)
			return
		}

		payment.CreatedAt =
			createdAt.Format(time.RFC3339)

		payments = append(
			payments,
			payment,
		)
	}

	if err := rows.Err(); err != nil {
		http.Error(
			w,
			"error reading payment rows: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	json.NewEncoder(w).Encode(
		payments,
	)
}
