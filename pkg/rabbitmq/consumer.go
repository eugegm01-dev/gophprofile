package rabbitmq

import (
	"context"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   string
}

func NewConsumer(url, queueName string) (*Consumer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Объявляем очередь (idempotent, должна совпадать с publisher)
	_, err = ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	// Ограничиваем prefetch (важно для гарантии доставки!)
	err = ch.Qos(1, 0, false)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	log.Printf("✅ Consumer connected to RabbitMQ, queue: %s", queueName)

	return &Consumer{
		conn:    conn,
		channel: ch,
		queue:   queueName,
	}, nil
}

func (c *Consumer) Close() {
	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	log.Println("🔒 Consumer RabbitMQ connection closed")
}

// Consume запускает бесконечный цикл чтения сообщений
func (c *Consumer) Consume(ctx context.Context, handler func([]byte) error) error {
	msgs, err := c.channel.Consume(
		c.queue,
		"",    // consumer tag
		false, // autoAck = false (ручное подтверждение!)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	log.Printf("🎧 Listening for messages on queue: %s", c.queue)

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Consumer stopping...")
			return nil
		case msg, ok := <-msgs:
			if !ok {
				return fmt.Errorf("channel closed")
			}

			log.Printf("📩 Received message: %s", string(msg.Body))

			// Вызываем обработчик
			if err := handler(msg.Body); err != nil {
				log.Printf("❌ Failed to process message: %v", err)
				// NACK без requeue — сообщение уйдет в DLQ (если настроена)
				if err := msg.Nack(false, false); err != nil {
					log.Printf("Failed to nack message: %v", err)
				}
				continue
			}

			// Успех — подтверждаем сообщение
			if err := msg.Ack(false); err != nil {
				log.Printf("Failed to ack message: %v", err)
			}
			log.Printf("✅ Message processed and acknowledged")
		}
	}
}
