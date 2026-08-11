package main

import (
	"context"
)


func applyPaymentEvent(ctx context.Context, paymentID string, event PaymentEvent, reason string) (PaymentState, error) {
	// START TRANSACTION
	// Get the current stateof the payment from the database
	// check if event is valid for the current state using NextPaymentState
	// if valid, update the state in the databse
	// COMMIT TRANSACTION
	// if any error occurs, ROLLBACK TRANSACTION -- this means that the database will not be changed at all
	// return the new state and any error that occurred
	// rollback means that if you are in the middle of a transaction and something
	//  goes wrong, you can undo all the changes you made in that transaction and go back
	// to the state before the transaction started. this is importatnt for maintaining data integrity.
	// for example, if you are transferring monety from one account to another,
	// you want to make sure that either both the debit and credit happen, or neither happens

	// ctx is a context.Context object that can be used to cancel the operation if it takes
	// too long or if the client disconnects. it is passed to database operations so that they
	// can be cancelled if needed. this is importatn for long-running operations or when
	// you want to make sure that resources are cleaned up if the client goes away.
	// so what heppens if you time out mid-payment? the transaction will be rolled back
	// and the payment will not be processed. this is important for maintaining data
	// integrity and consistency. if you were to allow a payment to be partially processed,
	// it could lead to inconsistencies in your database and potentially cause financial
	// loss or other issues. by rolling back the transaction, you ensure that either the
	// entire payment is processed successfully or not at all

	// start databse transaction, if anything fails rollback
	
	
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var currentState PaymentState

	err = tx.QueryRow(ctx, `
		SELECT status
		FROM payments
		WHERE id = $1::uuid
		FOR UPDATE
	`, paymentID).Scan(&currentState)
	if err != nil {
		return "", err
	}

	nextState, err := NextPaymentState(currentState, event)
	if err != nil {
		return "", err
	}

	var nextSequenceNumber int64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(sequence_number), 0) + 1
		FROM payment_transition_log
		WHERE payment_id = $1::uuid
	`, paymentID).Scan(&nextSequenceNumber)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(ctx, `
		UPDATE payments
		SET status = $1::varchar,
		    reject_reason = $2::text
		WHERE id = $3::uuid
	`, string(nextState), reason, paymentID)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO payment_transition_log (
			payment_id,
			sequence_number,
			from_state,
			to_state,
			event_type,
			reason
		)
		VALUES ($1::uuid, $2::bigint, $3::varchar, $4::varchar, $5::varchar, $6::text)
	`, paymentID, nextSequenceNumber, string(currentState), string(nextState), string(event), reason)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	return nextState, nil
}