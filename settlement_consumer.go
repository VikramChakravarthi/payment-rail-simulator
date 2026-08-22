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

func newSettlementConsumer() (*kgo.Client, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:9092"),
		kgo.ConsumeTopics(paymentEventsTopic),
		kgo.ConsumerGroup("settlement-worker"),
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
			"failed to create settlement consumer: %w",
			err,
		)
	}

	if err := client.Ping(context.Background()); err != nil {
		client.Close()

		return nil, fmt.Errorf(
			"failed to connect settlement consumer to Kafka: %w",
			err,
		)
	}

	return client, nil
}

func runSettlementConsumer(
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
					"settlement consumer Kafka error: %v",
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
				"invalid Kafka settlement event: %v",
				err,
			)

			if err := client.CommitRecords(ctx, record); err != nil {
				log.Printf(
					"failed to commit malformed settlement event: %v",
					err,
				)
			}

			continue
		}

		// settlement only cares about accepted payments.
		if event.EventType != "payment.accepted" {
			if err := client.CommitRecords(ctx, record); err != nil {
				log.Printf(
					"failed to commit ignored settlement event: %v",
					err,
				)
			}

			continue
		}

		for {
			result, err := settlePayment(
				ctx,
				event.PaymentID,
			)

			if err == nil {
				log.Printf(
					"automatically settled payment %s: status=%s",
					event.PaymentID,
					result.Status,
				)

				break
			}

			// Kafka may redeliver an event after settlement
			// already completed.
			if errors.Is(err, ErrPaymentNotAccepted) {
				log.Printf(
					"payment %s already moved past accepted; ignoring duplicate event",
					event.PaymentID,
				)

				break
			}

			log.Printf(
				"failed to settle payment %s: %v; retrying",
				event.PaymentID,
				err,
			)

			select {
			case <-ctx.Done():
				return

			case <-time.After(time.Second):
			}
		}

		// only commit after the event has been handled.
		if err := client.CommitRecords(ctx, record); err != nil {
			log.Printf(
				"failed to commit settlement Kafka event: %v",
				err,
			)
		}
	}
}