// Package report provides structured JSON report generation for YACMO experiments.
// After a chaos run, it writes a timestamped report with full details of every
// experiment including duration, success/failure, and resource targets.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"yacmo/pkg/logger"
)

// Report is the top-level report structure written after a chaos run.
type Report struct {
	Version     string            `json:"version"`
	GeneratedAt time.Time         `json:"generated_at"`
	DryRun      bool              `json:"dry_run"`
	Duration    string            `json:"total_duration"`
	Summary     Summary           `json:"summary"`
	Experiments []ExperimentEntry `json:"experiments"`
}

// Summary holds aggregate counts.
type Summary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// ExperimentEntry is one experiment's record in the report.
type ExperimentEntry struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // "success", "failed", "skipped"
	StartedAt time.Time `json:"started_at"`
	Duration  string    `json:"duration"`
	Error     string    `json:"error,omitempty"`
	Details   string    `json:"details,omitempty"`
}

// Builder accumulates experiment results and produces a Report.
type Builder struct {
	log       *logger.Logger
	startTime time.Time
	dryRun    bool
	entries   []ExperimentEntry
}

// NewBuilder creates a new report builder.
func NewBuilder(log *logger.Logger, dryRun bool) *Builder {
	return &Builder{
		log:       log,
		startTime: time.Now(),
		dryRun:    dryRun,
	}
}

// RecordExperiment adds an experiment result to the report.
func (b *Builder) RecordExperiment(name string, startedAt time.Time, duration time.Duration, err error, details string) {
	status := "success"
	errStr := ""
	if err != nil {
		status = "failed"
		errStr = err.Error()
	}
	if details == "dry-run: skipped" {
		status = "skipped"
	}

	b.entries = append(b.entries, ExperimentEntry{
		Name:      name,
		Status:    status,
		StartedAt: startedAt,
		Duration:  duration.String(),
		Error:     errStr,
		Details:   details,
	})
}

// Build creates the final Report.
func (b *Builder) Build() *Report {
	totalDuration := time.Since(b.startTime)

	summary := Summary{Total: len(b.entries)}
	for _, e := range b.entries {
		switch e.Status {
		case "success":
			summary.Succeeded++
		case "failed":
			summary.Failed++
		case "skipped":
			summary.Skipped++
		}
	}

	return &Report{
		Version:     "0.2.0",
		GeneratedAt: time.Now(),
		DryRun:      b.dryRun,
		Duration:    totalDuration.String(),
		Summary:     summary,
		Experiments: b.entries,
	}
}

// WriteJSON writes the report to a JSON file. If dir is empty, uses current directory.
func WriteJSON(report *Report, dir string, log *logger.Logger) (string, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating report dir: %w", err)
	}

	filename := fmt.Sprintf("yacmo-report-%s.json",
		report.GeneratedAt.Format("20060102-150405"))
	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshalling report: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("writing report: %w", err)
	}

	log.Info("📄 Report written to %s", path)
	return path, nil
}
