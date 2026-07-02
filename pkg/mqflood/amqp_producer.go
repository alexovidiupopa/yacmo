package mqflood

import (
	"context"
	"fmt"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// AMQPProducer implements MQProducer for RabbitMQ via AMQP 0-9-1.
type AMQPProducer struct {
	cfg  config.MQTarget
	log  *logger.Logger
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewAMQPProducer creates a new RabbitMQ producer.
func NewAMQPProducer(cfg config.MQTarget, log *logger.Logger) *AMQPProducer {
	return &AMQPProducer{cfg: cfg, log: log}
}

func (p *AMQPProducer) Connect(_ context.Context) error {
	url := p.cfg.BrokerURL
	if url == "" {
		url = "amqp://guest:guest@localhost:5672/"
	}

	var err error
	p.conn, err = amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}

	p.ch, err = p.conn.Channel()
	if err != nil {
		p.conn.Close()
		return fmt.Errorf("amqp channel: %w", err)
	}

	// Declare the queue/exchange if a queue name is given
	if p.cfg.Queue != "" {
		_, err = p.ch.QueueDeclare(
			p.cfg.Queue,
			true,  // durable
			false, // auto-delete
			false, // exclusive
			false, // no-wait
			nil,
		)
		if err != nil {
			return fmt.Errorf("amqp queue declare: %w", err)
		}
	}

	p.log.Info("Connected to RabbitMQ at %s", url)
	return nil
}

func (p *AMQPProducer) Publish(ctx context.Context, topic string, data []byte) error {
	exchange := ""
	routingKey := topic
	if p.cfg.Queue != "" {
		routingKey = p.cfg.Queue
	}

	return p.ch.PublishWithContext(ctx,
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType: "application/octet-stream",
			Body:        data,
		},
	)
}

func (p *AMQPProducer) Close() error {
	if p.ch != nil {
		p.ch.Close()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}
