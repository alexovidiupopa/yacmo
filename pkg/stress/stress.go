// Package stress provides system resource stress chaos experiments.
// It can burn CPU, exhaust memory, thrash disk I/O, and fill disk space
// to simulate resource contention scenarios.
package stress

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
	"yacmo/pkg/safety"
)

// ChaosStress implements chaos.Experiment for resource stress testing.
type ChaosStress struct {
	cfg       config.StressConfig
	log       *logger.Logger
	policy    *safety.Policy
	tempFiles []string // track files created for disk fill (for rollback)
}

// New creates a new stress chaos experiment.
func New(cfg config.StressConfig, policy *safety.Policy, log *logger.Logger) *ChaosStress {
	return &ChaosStress{
		cfg:    cfg,
		log:    log,
		policy: policy,
	}
}

// Name returns the experiment name.
func (c *ChaosStress) Name() string {
	return fmt.Sprintf("resource-stress[actions=%s, duration=%s]",
		strings.Join(c.cfg.Actions, ","), c.cfg.Duration)
}

// Run executes the resource stress experiment.
func (c *ChaosStress) Run(ctx context.Context) error {
	duration := c.cfg.Duration
	if duration <= 0 {
		duration = 30 * time.Second
	}

	stressCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(c.cfg.Actions))

	for _, action := range c.cfg.Actions {
		switch action {
		case "cpu":
			if err := c.checkAction("cpu"); err != nil {
				return err
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- c.stressCPU(stressCtx)
			}()
		case "memory":
			if err := c.checkAction("memory"); err != nil {
				return err
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- c.stressMemory(stressCtx)
			}()
		case "disk_io":
			if err := c.checkAction("disk_io"); err != nil {
				return err
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- c.stressDiskIO(stressCtx)
			}()
		case "disk_fill":
			if err := c.checkAction("disk_fill"); err != nil {
				return err
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- c.stressDiskFill(stressCtx)
			}()
		default:
			c.log.Warn("Unknown stress action: %s", action)
		}
	}

	wg.Wait()
	close(errCh)

	var errs []string
	for err := range errCh {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("stress errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// DestructiveActionCount returns the number of destructive stress actions configured.
func (c *ChaosStress) DestructiveActionCount() int {
	return len(c.cfg.Actions)
}

// Rollback cleans up any resources created during stress (e.g., temp files).
func (c *ChaosStress) Rollback(_ context.Context) error {
	var errs []string
	for _, f := range c.tempFiles {
		c.log.Info("Removing temp file: %s", f)
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("remove %s: %v", f, err))
		}
	}
	c.tempFiles = nil
	if len(errs) > 0 {
		return fmt.Errorf("rollback errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// stressCPU burns CPU by running tight loops across multiple goroutines.
func (c *ChaosStress) stressCPU(ctx context.Context) error {
	cores := c.cfg.CPUCores
	if cores <= 0 {
		cores = runtime.NumCPU()
	}
	c.log.Info("🔥 Burning CPU on %d cores for %s", cores, c.cfg.Duration)

	var wg sync.WaitGroup
	for i := 0; i < cores; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Tight loop — burn CPU
					x := 0
					for j := 0; j < 1_000_000; j++ {
						x += j * j
					}
					_ = x
				}
			}
		}()
	}

	wg.Wait()
	c.log.Info("🔥 CPU stress completed")
	return nil
}

// stressMemory allocates and holds memory to create memory pressure.
func (c *ChaosStress) stressMemory(ctx context.Context) error {
	targetMB := c.cfg.MemoryMB
	if targetMB <= 0 {
		targetMB = 256
	}
	c.log.Info("🧠 Allocating %d MB of memory for %s", targetMB, c.cfg.Duration)

	// Allocate in 1MB chunks to avoid one huge allocation
	chunkSize := 1024 * 1024 // 1 MB
	chunks := make([][]byte, 0, targetMB)

	for i := 0; i < targetMB; i++ {
		select {
		case <-ctx.Done():
			c.log.Info("🧠 Memory stress stopped (allocated %d MB)", i)
			return nil
		default:
		}
		chunk := make([]byte, chunkSize)
		// Touch every page to ensure the OS actually allocates it
		for j := 0; j < len(chunk); j += 4096 {
			chunk[j] = byte(i)
		}
		chunks = append(chunks, chunk)
	}

	c.log.Info("🧠 Holding %d MB — waiting for duration to expire", targetMB)
	<-ctx.Done()

	// Keep reference alive until context ends
	runtime.KeepAlive(chunks)
	c.log.Info("🧠 Memory stress completed, releasing %d MB", targetMB)
	return nil
}

// stressDiskIO performs random read/write I/O to create disk pressure.
func (c *ChaosStress) stressDiskIO(ctx context.Context) error {
	workers := c.cfg.DiskIOWorkers
	if workers <= 0 {
		workers = 4
	}
	dir := c.cfg.TempDir
	if dir == "" {
		dir = os.TempDir()
	}

	c.log.Info("💾 Stressing disk I/O with %d workers in %s", workers, dir)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			path := filepath.Join(dir, fmt.Sprintf("yacmo-diskio-%d.tmp", id))
			c.tempFiles = append(c.tempFiles, path)

			buf := make([]byte, 4096)
			for {
				select {
				case <-ctx.Done():
					os.Remove(path)
					return
				default:
				}

				rand.Read(buf)
				if err := os.WriteFile(path, buf, 0644); err != nil {
					c.log.Debug("disk io write error: %v", err)
					continue
				}
				if _, err := os.ReadFile(path); err != nil {
					c.log.Debug("disk io read error: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()
	c.log.Info("💾 Disk I/O stress completed")
	return nil
}

// stressDiskFill creates a large temporary file to consume disk space.
func (c *ChaosStress) stressDiskFill(ctx context.Context) error {
	sizeMB := c.cfg.DiskFillMB
	if sizeMB <= 0 {
		sizeMB = 512
	}
	dir := c.cfg.TempDir
	if dir == "" {
		dir = os.TempDir()
	}

	path := filepath.Join(dir, "yacmo-diskfill.tmp")
	c.tempFiles = append(c.tempFiles, path)

	c.log.Info("📀 Filling disk with %d MB at %s", sizeMB, path)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create disk fill file: %w", err)
	}
	defer f.Close()

	chunk := make([]byte, 1024*1024) // 1 MB
	rand.Read(chunk)

	for i := 0; i < sizeMB; i++ {
		select {
		case <-ctx.Done():
			c.log.Info("📀 Disk fill stopped at %d MB", i)
			return nil
		default:
		}
		if _, err := f.Write(chunk); err != nil {
			return fmt.Errorf("disk fill write at %d MB: %w", i, err)
		}
	}

	c.log.Info("📀 Disk fill complete (%d MB), holding until duration expires", sizeMB)
	<-ctx.Done()
	c.log.Info("📀 Disk fill stress completed")
	return nil
}

func (c *ChaosStress) checkAction(action string) error {
	if c.policy == nil {
		return nil
	}
	return c.policy.CheckStressAction(action)
}
