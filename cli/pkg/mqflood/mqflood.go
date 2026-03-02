// Package mqflood provides message queue traffic injection chaos experiments.
// It supports RabbitMQ (AMQP), Kafka, and NATS backends.
package mqflood

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// MQProducer is an interface for sending messages to a queue/topic.
type MQProducer interface {
	// Connect establishes a connection to the broker.
	Connect(ctx context.Context) error
	// Publish sends a single message.
	Publish(ctx context.Context, topic string, data []byte) error
	// Close tears down the connection.
	Close() error
}

// Stats holds aggregate statistics for an MQ injection run.
type Stats struct {
	TotalMessages int64
	SuccessCount  int64
	ErrorCount    int64
	TotalBytes    int64
}

// ChaosMQ implements chaos.Experiment for MQ traffic injection.
type ChaosMQ struct {
	cfg       config.MQConfig
	log       *logger.Logger
	producers map[string]MQProducer // registered external producers (for extensibility)
}

// New creates a new MQ chaos experiment.
func New(cfg config.MQConfig, log *logger.Logger) *ChaosMQ {
	return &ChaosMQ{
		cfg:       cfg,
		log:       log,
		producers: make(map[string]MQProducer),
	}
}

// RegisterProducer allows injecting a custom producer for a backend name.
// This is useful for testing or for integrating custom MQ clients.
func (c *ChaosMQ) RegisterProducer(name string, p MQProducer) {
	c.producers[name] = p
}

// Name returns the experiment name.
func (c *ChaosMQ) Name() string {
	var names []string
	for _, b := range c.cfg.Backends {
		names = append(names, fmt.Sprintf("%s(%s)", b.Name, b.Type))
	}
	return fmt.Sprintf("mq-flood[backends=%s]", strings.Join(names, ","))
}

// Run executes the MQ traffic injection experiment.
func (c *ChaosMQ) Run(ctx context.Context) error {
	for _, backend := range c.cfg.Backends {
		c.log.Info("📨 Starting MQ injection against %s [%s] at %s",
			backend.Name, backend.Type, backend.BrokerURL)

		producer, err := c.getProducer(ctx, backend)
		if err != nil {
			return fmt.Errorf("mq connect %s: %w", backend.Name, err)
		}

		stats, err := c.flood(ctx, backend, producer)

		// Always try to close
		if closeErr := producer.Close(); closeErr != nil {
			c.log.Warn("Error closing producer %s: %v", backend.Name, closeErr)
		}

		if err != nil {
			return fmt.Errorf("mq flood %s: %w", backend.Name, err)
		}

		c.log.Info("📊 %s results: total=%d success=%d errors=%d bytes=%d",
			backend.Name, stats.TotalMessages, stats.SuccessCount, stats.ErrorCount, stats.TotalBytes)
	}
	return nil
}

// Rollback is a no-op for MQ injection (messages already sent).
func (c *ChaosMQ) Rollback(_ context.Context) error {
	c.log.Info("MQ injection rollback: nothing to undo (messages already sent)")
	return nil
}

// getProducer returns an MQProducer for the given backend, either from the
// registered producers or by creating the appropriate built-in one.
func (c *ChaosMQ) getProducer(ctx context.Context, backend config.MQTarget) (MQProducer, error) {
	// Check for a registered custom producer first
	if p, ok := c.producers[backend.Name]; ok {
		if err := p.Connect(ctx); err != nil {
			return nil, err
		}
		return p, nil
	}

	// Create a built-in producer based on type
	var producer MQProducer
	switch backend.Type {
	case "rabbitmq":
		producer = NewAMQPProducer(backend, c.log)
	case "kafka":
		producer = NewKafkaProducer(backend, c.log)
	case "nats":
		producer = NewNATSProducer(backend, c.log)
	default:
		return nil, fmt.Errorf("unsupported MQ type: %s", backend.Type)
	}

	if err := producer.Connect(ctx); err != nil {
		return nil, err
	}
	return producer, nil
}

// flood sends concurrent messages to a single MQ backend.
func (c *ChaosMQ) flood(ctx context.Context, backend config.MQTarget, producer MQProducer) (*Stats, error) {
	concurrency := backend.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	messageCount := backend.MessageCount
	if messageCount <= 0 {
		messageCount = 100
	}
	messageSize := backend.MessageSize
	if messageSize <= 0 {
		messageSize = 256
	}

	topic := backend.Topic
	if topic == "" {
		topic = backend.Queue
	}

	var stats Stats

	// Duration-limited context
	floodCtx := ctx
	if backend.Duration > 0 {
		var cancel context.CancelFunc
		floodCtx, cancel = context.WithTimeout(ctx, backend.Duration)
		defer cancel()
	}

	// Rate limiter
	var rateTicker *time.Ticker
	if backend.RatePerSec > 0 {
		interval := time.Duration(float64(time.Second) / backend.RatePerSec)
		rateTicker = time.NewTicker(interval)
		defer rateTicker.Stop()
	}

	work := make(chan int, concurrency)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				select {
				case <-floodCtx.Done():
					return
				default:
				}

				payload := c.buildPayload(backend, messageSize)
				err := producer.Publish(floodCtx, topic, payload)
				atomic.AddInt64(&stats.TotalMessages, 1)

				if err != nil {
					atomic.AddInt64(&stats.ErrorCount, 1)
					c.log.Debug("MQ publish error: %v", err)
				} else {
					atomic.AddInt64(&stats.SuccessCount, 1)
					atomic.AddInt64(&stats.TotalBytes, int64(len(payload)))
				}
			}
		}()
	}

	// Dispatch
	go func() {
		defer close(work)
		for i := 0; i < messageCount; i++ {
			select {
			case <-floodCtx.Done():
				return
			default:
			}
			if rateTicker != nil {
				select {
				case <-rateTicker.C:
				case <-floodCtx.Done():
					return
				}
			}
			select {
			case work <- i:
			case <-floodCtx.Done():
				return
			}
		}
	}()

	wg.Wait()
	return &stats, nil
}

// buildPayload creates a message payload.
func (c *ChaosMQ) buildPayload(backend config.MQTarget, size int) []byte {
	if backend.RandomPayload || backend.PayloadPattern == "" {
		buf := make([]byte, size)
		rand.Read(buf)
		return buf
	}
	// Repeat the pattern to fill the desired size
	pattern := []byte(backend.PayloadPattern)
	buf := make([]byte, 0, size)
	for len(buf) < size {
		remaining := size - len(buf)
		if remaining < len(pattern) {
			buf = append(buf, pattern[:remaining]...)
		} else {
			buf = append(buf, pattern...)
		}
	}
	return buf
}
