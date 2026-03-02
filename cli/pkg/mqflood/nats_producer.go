package mqflood

import (
	"context"
	"fmt"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"

	"github.com/nats-io/nats.go"
)

// NATSProducer implements MQProducer for NATS.
type NATSProducer struct {
	cfg  config.MQTarget
	log  *logger.Logger
	conn *nats.Conn
}

// NewNATSProducer creates a new NATS producer.
func NewNATSProducer(cfg config.MQTarget, log *logger.Logger) *NATSProducer {
	return &NATSProducer{cfg: cfg, log: log}
}

func (p *NATSProducer) Connect(_ context.Context) error {
	url := p.cfg.BrokerURL
	if url == "" {
		url = nats.DefaultURL
	}

	opts := []nats.Option{nats.Name("yacmo-chaos")}
	if p.cfg.Username != "" {
		opts = append(opts, nats.UserInfo(p.cfg.Username, p.cfg.Password))
	}

	var err error
	p.conn, err = nats.Connect(url, opts...)
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}

	p.log.Info("Connected to NATS at %s", url)
	return nil
}

func (p *NATSProducer) Publish(_ context.Context, topic string, data []byte) error {
	return p.conn.Publish(topic, data)
}

func (p *NATSProducer) Close() error {
	if p.conn != nil {
		p.conn.Close()
	}
	return nil
}
