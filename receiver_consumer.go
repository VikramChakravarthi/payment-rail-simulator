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

func newReceiverConsumer() (*kgo.Client, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:9092"),
		kgo.ConsumeTopics(paymentEventsTopic),
		kgo.ConsumerGroup("receiver-worker"),
		kgo.DisableAutoCommit(),

		kgo.ConsumeStartOffset(
			kgo.NewOffset().AtEnd(),
		),

		kgo.ConsumeResetOffset(
			kgo.NewOffset().AtEnd(),
		),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"failed to create receiver consumer: %w",
			err,
		)
	}

	if err := client.Ping(context.Background()); err != nil {
		client.Close()

		return nil, fmt.Errorf(
			"failed to connect receiver consumer to Kafka: %w",
			err,
		)
	}

	return client, nil
}

func runReceiverConsumer(
	ctx context.Context,
	client *kgo.Client,
) {
	for {
		fetches := client.PollRecords(ctx, 1)

		if ctx.Err() != nil {
			return
		}

		if errs := fetches.Errors(); len(errs) > 0 {
			for _, fetchErr := range errs {
				log.Printf(
					"receiver consumer Kafka error: %v",
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

			if err := client.CommitRecords(ctx, record); err != nil {
				log.Printf(
					"failed to commit malformed Kafka event: %v",
					err,
				)
			}

			continue
		}

		// This worker only reacts to successfully cleared payments.
		if event.EventType != "payment.cleared" {
			if err := client.CommitRecords(ctx, record); err != nil {
				log.Printf(
					"failed to commit ignored receiver event: %v",
					err,
				)
			}

			continue
		}

		for {
			// For now our simulated receiving bank always accepts.
			result, err := respondToPayment(
				ctx,
				event.PaymentID,
				true,
				"",
			)

			if err == nil {
				log.Printf(
					"receiver automatically accepted payment %s: status=%s",
					event.PaymentID,
					result.Status,
				)

				break
			}

			// Duplicate delivery: payment has already moved on.
			if errors.Is(err, ErrPaymentNotAwaitingResponse) {
				log.Printf(
					"payment %s already moved past awaiting_response; ignoring duplicate event",
					event.PaymentID,
				)

				break
			}

			log.Printf(
				"failed to process receiver response for payment %s: %v; retrying",
				event.PaymentID,
				err,
			)

			select {
			case <-ctx.Done():
				return

			case <-time.After(time.Second):
			}
		}

		if err := client.CommitRecords(ctx, record); err != nil {
			log.Printf(
				"failed to commit receiver Kafka event: %v",
				err,
			)
		}
	}
}
