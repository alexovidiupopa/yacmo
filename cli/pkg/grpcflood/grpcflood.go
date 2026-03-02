// Package grpcflood provides gRPC traffic injection chaos experiments.
// It can flood gRPC endpoints with unary calls using configurable concurrency,
// rate limiting, and payload sizes.
package grpcflood

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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// ChaosGRPC implements chaos.Experiment for gRPC traffic injection.
type ChaosGRPC struct {
	cfg config.GRPCConfig
	log *logger.Logger
}

// Stats holds aggregate statistics for a gRPC injection run.
type Stats struct {
	TotalRequests  int64
	SuccessCount   int64
	ErrorCount     int64
	TotalLatencyMs int64
}

// New creates a new gRPC chaos experiment.
func New(cfg config.GRPCConfig, log *logger.Logger) *ChaosGRPC {
	return &ChaosGRPC{cfg: cfg, log: log}
}

// Name returns the experiment name.
func (c *ChaosGRPC) Name() string {
	var names []string
	for _, t := range c.cfg.Targets {
		names = append(names, t.Name)
	}
	return fmt.Sprintf("grpc-flood[targets=%s]", strings.Join(names, ","))
}

// Run executes the gRPC traffic injection experiment.
func (c *ChaosGRPC) Run(ctx context.Context) error {
	for _, target := range c.cfg.Targets {
		c.log.Info("⚡ Starting gRPC injection against %s (%s/%s)",
			target.Name, target.Address, target.Method)

		stats, err := c.flood(ctx, target)
		if err != nil {
			return fmt.Errorf("grpc flood %s: %w", target.Name, err)
		}

		avgLatency := float64(0)
		if stats.TotalRequests > 0 {
			avgLatency = float64(stats.TotalLatencyMs) / float64(stats.TotalRequests)
		}

		c.log.Info("📊 %s results: total=%d success=%d errors=%d avg_latency=%.1fms",
			target.Name, stats.TotalRequests, stats.SuccessCount, stats.ErrorCount, avgLatency)
	}
	return nil
}

// Rollback is a no-op for gRPC injection.
func (c *ChaosGRPC) Rollback(_ context.Context) error {
	c.log.Info("gRPC injection rollback: nothing to undo (requests already sent)")
	return nil
}

// flood sends concurrent gRPC requests to a single target.
func (c *ChaosGRPC) flood(ctx context.Context, target config.GRPCTarget) (*Stats, error) {
	concurrency := target.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	totalRequests := target.TotalRequests
	if totalRequests <= 0 {
		totalRequests = 100
	}

	// Build dial options
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(16 * 1024 * 1024)),
	}
	if target.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(target.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", target.Address, err)
	}
	defer conn.Close()

	var stats Stats

	floodCtx := ctx
	if target.Duration > 0 {
		var cancel context.CancelFunc
		floodCtx, cancel = context.WithTimeout(ctx, target.Duration)
		defer cancel()
	}

	var rateTicker *time.Ticker
	if target.RatePerSecond > 0 {
		interval := time.Duration(float64(time.Second) / target.RatePerSecond)
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

				// Build the request payload
				var payload []byte
				if target.RandomPayload && target.PayloadSizeBytes > 0 {
					payload = make([]byte, target.PayloadSizeBytes)
					rand.Read(payload)
				} else if target.Payload != "" {
					payload = []byte(target.Payload)
				} else {
					payload = []byte("{}")
				}

				// Add metadata (headers)
				callCtx := floodCtx
				if len(target.Metadata) > 0 {
					md := metadata.New(target.Metadata)
					callCtx = metadata.NewOutgoingContext(floodCtx, md)
				}

				start := time.Now()
				var response []byte
				err := conn.Invoke(callCtx, target.Method, payload, &response)
				elapsed := time.Since(start).Milliseconds()

				atomic.AddInt64(&stats.TotalLatencyMs, elapsed)
				atomic.AddInt64(&stats.TotalRequests, 1)

				if err != nil {
					atomic.AddInt64(&stats.ErrorCount, 1)
					c.log.Debug("gRPC error: %v", err)
				} else {
					atomic.AddInt64(&stats.SuccessCount, 1)
				}
			}
		}()
	}

	go func() {
		defer close(work)
		for i := 0; i < totalRequests; i++ {
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
