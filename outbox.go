package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type OutboxEvent struct {
	ID        string
	PaymentID string
	EventType string
	PayLoad   string
}

func recordPaymentValidatedOutboxTx(
	ctx context.Context,
	tx pgx.Tx,
	paymentID string,
	uetr string,
) error {

	payload := map[string]string{
		"payment_id": paymentID,
		"uetr":       uetr,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to create outbox payload: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (
			payment_id,
			event_type,
			payload
		)
		VALUES (
			$1::uuid,
			$2::varchar,
			$3::jsonb
		)
	`,
		paymentID,
		"payment.validated",
		string(payloadJSON),
	)

	if err != nil {
		return fmt.Errorf(
			"failed to record payment validated outbox event: %w",
			err,
		)
	}

	return nil
}

func getNextUnpublishedOutboxEvent(
	ctx context.Context) (*OutboxEvent, error) {
	var event OutboxEvent

	err := db.QueryRow(ctx, `
			SELECT
				id::text,
				payment_id::text,
				event_type,
				payload::text
			FROM outbox_events
			WHERE published_at IS NULL
			ORDER BY created_at ASC
			LIMIT 1
		`).Scan(
		&event.ID,
		&event.PaymentID,
		&event.EventType,
		&event.PayLoad,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read unpublished outbox event: %w", err)
	}

	return &event, nil
}

func markOutboxEventPublished(
	ctx context.Context,
	eventID string,
) error {

	_, err := db.Exec(ctx, `
		UPDATE outbox_events
		SET published_at = now()
		WHERE id = $1::uuid
	`, eventID)

	if err != nil {
		return fmt.Errorf(
			"failed to mark outbox event as published: %w",
			err,
		)
	}

	return nil
}

func recordPaymentClearedOutboxTx(
	ctx context.Context,
	tx pgx.Tx,
	paymentID string,
) error {

	payload := map[string]string{
		"payment_id": paymentID,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf(
			"failed to create payment cleared payload: %w",
			err,
		)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (
			payment_id,
			event_type,
			payload
		)
		VALUES (
			$1::uuid,
			$2::varchar,
			$3::jsonb
		)
	`,
		paymentID,
		"payment.cleared",
		string(payloadJSON),
	)

	if err != nil {
		return fmt.Errorf(
			"failed to record payment cleared outbox event: %w",
			err,
		)
	}

	return nil
}


func recordPaymentAcceptedOutboxTx(
	ctx context.Context,
	tx pgx.Tx,
	paymentID string,
) error {

	payload := map[string]string{
		"payment_id": paymentID,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf(
			"failed to create payment accepted payload: %w",
			err,
		)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (
			payment_id,
			event_type,
			payload
		)
		VALUES (
			$1::uuid,
			$2::varchar,
			$3::jsonb
		)
	`,
		paymentID,
		"payment.accepted",
		string(payloadJSON),
	)

	if err != nil {
		return fmt.Errorf(
			"failed to record payment accepted outbox event: %w",
			err,
		)
	}

	return nil
}
