package mqflood

import (
	"context"
	"fmt"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"

	"github.com/IBM/sarama"
)

// KafkaProducer implements MQProducer for Apache Kafka.
type KafkaProducer struct {
	cfg      config.MQTarget
	log      *logger.Logger
	producer sarama.SyncProducer
}

// NewKafkaProducer creates a new Kafka producer.
func NewKafkaProducer(cfg config.MQTarget, log *logger.Logger) *KafkaProducer {
	return &KafkaProducer{cfg: cfg, log: log}
}

func (p *KafkaProducer) Connect(_ context.Context) error {
	kafkaCfg := sarama.NewConfig()
	kafkaCfg.Producer.Return.Successes = true
	kafkaCfg.Producer.RequiredAcks = sarama.WaitForAll
	kafkaCfg.Producer.Retry.Max = 3

	if p.cfg.Username != "" {
		kafkaCfg.Net.SASL.Enable = true
		kafkaCfg.Net.SASL.User = p.cfg.Username
		kafkaCfg.Net.SASL.Password = p.cfg.Password
		kafkaCfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	}

	brokers := []string{p.cfg.BrokerURL}

	var err error
	p.producer, err = sarama.NewSyncProducer(brokers, kafkaCfg)
	if err != nil {
		return fmt.Errorf("kafka connect: %w", err)
	}

	p.log.Info("Connected to Kafka at %s", p.cfg.BrokerURL)
	return nil
}

func (p *KafkaProducer) Publish(_ context.Context, topic string, data []byte) error {
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(data),
	}
	_, _, err := p.producer.SendMessage(msg)
	return err
}

func (p *KafkaProducer) Close() error {
	if p.producer != nil {
		return p.producer.Close()
	}
	return nil
}
