package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func newKafkaClient() (*kgo.Client, error) {
	// client is the go programs connection to Kafka
	client, err := kgo.NewClient(kgo.SeedBrokers("localhost:9092")) // creates an object that knows how to communicate with Kafka on port 9092

	if err != nil {
		return nil, fmt.Errorf(
			"failed to create Kafka client: %w", err,
		)
	}

	if err := client.Ping(context.Background()); err != nil { // checking if kafka broker can be reached
		client.Close()

		return nil, fmt.Errorf(
			"failed to connect to Kafka: %w", err,
		)
	}

	return client, nil
}

const paymentEventsTopic = "payment-events"

type KafkaPaymentEvent struct {
	EventID   string          `json:"event_id"`
	PaymentID string          `json:"payment_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

func publishOutboxEvent(
	ctx context.Context,
	client *kgo.Client,
	event *OutboxEvent,
) error {

	kafkaEvent := KafkaPaymentEvent{
		EventID:   event.ID,
		PaymentID: event.PaymentID,
		EventType: event.EventType,
		Payload:   json.RawMessage(event.PayLoad),
	}

	message, err := json.Marshal(kafkaEvent)
	if err != nil {
		return fmt.Errorf(
			"failed to create Kafka message: %w",
			err,
		)
	}

	result := client.ProduceSync(
		ctx,
		&kgo.Record{
			Topic: paymentEventsTopic,
			Key:   []byte(event.PaymentID),
			Value: message,
		},
	)

	if err := result.FirstErr(); err != nil {
		return fmt.Errorf(
			"failed to publish event to Kafka: %w",
			err,
		)
	}

	return nil
}

func runOutboxPublisher(
	ctx context.Context,
	client *kgo.Client,
) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		// Keep publishing until there are no more
		// unpublished events waiting.
		for {
			event, err := getNextUnpublishedOutboxEvent(ctx)

			if err != nil {
				log.Printf(
					"failed to read outbox event: %v",
					err,
				)
				break
			}

			// No unpublished events currently exist.
			if event == nil {
				break
			}

			// Send the event to Kafka.
			err = publishOutboxEvent(
				ctx,
				client,
				event,
			)

			if err != nil {
				log.Printf(
					"failed to publish outbox event %s: %v",
					event.ID,
					err,
				)
				break
			}

			// Kafka accepted it, so mark it as published.
			err = markOutboxEventPublished(
				ctx,
				event.ID,
			)

			if err != nil {
				log.Printf(
					"failed to mark outbox event %s as published: %v",
					event.ID,
					err,
				)
				break
			}

			log.Printf(
				"published outbox event %s (%s) for payment %s",
				event.ID,
				event.EventType,
				event.PaymentID,
			)
		}

		// Wait either:
		// 1. one second, then check again
		// 2. until the application shuts down
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
		}
	}
}
