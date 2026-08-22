package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func newClearingConsumer() (*kgo.Client, error) {
	client, err := kgo.NewClient(

		kgo.SeedBrokers(kafkaBrokers()...),

		// Listen to our payment event stream.
		kgo.ConsumeTopics(paymentEventsTopic),

		// Kafka remembers this worker's position using this group.
		kgo.ConsumerGroup("clearing-worker"),

		// We decide when a message has been successfully processed.
		kgo.DisableAutoCommit(),

		kgo.BlockRebalanceOnPoll(),

		// For this first run, ignore the old demo messages already
		// sitting in Kafka and begin with new messages.
		kgo.ConsumeStartOffset(
			kgo.NewOffset().AtStart(),
		),
		kgo.ConsumeResetOffset(
			kgo.NewOffset().AtStart(),
		),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"failed to create clearing consumer: %w",
			err,
		)
	}

	if err := client.Ping(context.Background()); err != nil {
		client.Close()

		return nil, fmt.Errorf(
			"failed to connect clearing consumer to Kafka: %w",
			err,
		)
	}

	return client, nil
}

func runClearingConsumer(
	ctx context.Context,
	client *kgo.Client,
) {
	for {
		// wait for one Kafka message
		fetches := client.PollRecords(ctx, 1)

		if ctx.Err() != nil {
			return
		}

		// kafka itself returned an error
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, fetchErr := range errs {
				log.Printf(
					"clearing consumer Kafka error: %v",
					fetchErr,
				)
			}

			continue
		}

		records := fetches.Records()

		if len(records) == 0 {
			continue
		}

		record := records[0]

		var event KafkaPaymentEvent

		err := json.Unmarshal(
			record.Value,
			&event,
		)

		if err != nil {
			log.Printf(
				"invalid Kafka payment event: %v",
				err,
			)

			// the message is malformed and cannot be processed
			// commit it so it does not block this simple worker forever
			if err := client.CommitRecords(ctx, record); err != nil {
				log.Printf(
					"failed to commit malformed Kafka event: %v",
					err,
				)
			}

			continue
		}

		// this worker only handles payment.validated
		if event.EventType != "payment.validated" {
			if err := client.CommitRecords(ctx, record); err != nil {
				log.Printf(
					"failed to commit ignored Kafka event: %v",
					err,
				)
			}

			continue
		}

		// keep trying this same payment until clearing succeeds
		// or the application shuts down
		for {
			result, err := clearPayment(
				ctx,
				event.PaymentID,
			)

			if err == nil {
				log.Printf(
					"automatically cleared payment %s: status=%s",
					event.PaymentID,
					result.Status,
				)

				break
			}

			// this can happen if Kafka delivers the same event again
			// after the payment was already processed
			if errors.Is(err, ErrPaymentNotValidated) {
				log.Printf(
					"payment %s already moved past validated; ignoring duplicate event",
					event.PaymentID,
				)

				break
			}

			log.Printf(
				"failed to clear payment %s: %v; retrying",
				event.PaymentID,
				err,
			)

			select {
			case <-ctx.Done():
				return

			case <-time.After(time.Second):
			}
		}

		// clearing has been handled
		// tell Kafka: "we are finished with this message"
		if err := client.CommitRecords(ctx, record); err != nil {
			log.Printf(
				"failed to commit clearing Kafka event: %v",
				err,
			)
		}
	}
}
