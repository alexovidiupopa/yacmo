// Package report provides structured JSON report generation for YACMO experiments.
// After a chaos run, it writes a timestamped report with full details of every
// experiment including duration, success/failure, and resource targets.
package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Format      string            `json:"format"`
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
	format    string
}

// NewBuilder creates a new report builder.
func NewBuilder(log *logger.Logger, dryRun bool, format string) *Builder {
	return &Builder{
		log:       log,
		startTime: time.Now(),
		dryRun:    dryRun,
		format:    format,
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
		Format:      b.format,
	}
}

// Write writes the report to a file, depending on the configured format. If dir is empty, uses current directory.
func Write(report *Report, dir string, log *logger.Logger) (string, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating report dir: %w", err)
	}

	filename := fmt.Sprintf("yacmo-report-%s.%s",
		report.GeneratedAt.Format("20060102-150405"), report.Format)
	path := filepath.Join(dir, filename)

	var data []byte
	var err error

	switch report.Format {
	case "json":
		data, err = writeJson(report)
		if err != nil {
			return "", err
		}
	case "html":
		data, err = writeHtml(report)
		if err != nil {
			return "", err
		}
	case "csv":
		data, err = writeCsv(report)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported format: %s", report.Format)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("writing report: %w", err)
	}

	log.Info("📄 Report written to %s", path)
	return path, nil
}

func writeCsv(report *Report) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// Write header
	if err := w.Write([]string{"Name", "Status", "Started At", "Duration", "Error", "Details"}); err != nil {
		return make([]byte, 0), fmt.Errorf("writing CSV header: %w", err)
	}

	// Write rows
	for _, e := range report.Experiments {
		if err := w.Write([]string{
			e.Name,
			e.Status,
			e.StartedAt.Format(time.RFC3339),
			e.Duration,
			e.Error,
			e.Details,
		}); err != nil {
			return make([]byte, 0), fmt.Errorf("writing CSV row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return make([]byte, 0), fmt.Errorf("flushing CSV: %w", err)
	}

	return buf.Bytes(), nil
}

func writeHtml(report *Report) ([]byte, error) {
	var buf bytes.Buffer

	// Write HTML header
	buf.WriteString("<!DOCTYPE html>\n")
	buf.WriteString("<html lang=\"en\">\n")
	buf.WriteString("<head>\n")
	buf.WriteString("  <meta charset=\"UTF-8\">\n")
	buf.WriteString("  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n")
	buf.WriteString("  <title>YACMO Report</title>\n")
	buf.WriteString("  <style>\n")
	buf.WriteString("    * { margin: 0; padding: 0; box-sizing: border-box; }\n")
	buf.WriteString("    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif; background: #f5f5f5; color: #333; }\n")
	buf.WriteString("    .container { max-width: 1200px; margin: 0 auto; padding: 20px; }\n")
	buf.WriteString("    header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 40px; border-radius: 8px; margin-bottom: 30px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }\n")
	buf.WriteString("    h1 { font-size: 2.5em; margin-bottom: 10px; }\n")
	buf.WriteString("    .header-meta { display: flex; flex-wrap: wrap; gap: 30px; margin-top: 20px; font-size: 0.95em; }\n")
	buf.WriteString("    .meta-item { display: flex; flex-direction: column; }\n")
	buf.WriteString("    .meta-label { opacity: 0.9; font-weight: 600; font-size: 0.85em; text-transform: uppercase; letter-spacing: 0.5px; }\n")
	buf.WriteString("    .meta-value { font-size: 1.1em; font-weight: 500; margin-top: 4px; }\n")
	buf.WriteString("    .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; margin-bottom: 30px; }\n")
	buf.WriteString("    .summary-card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); text-align: center; border-left: 4px solid #667eea; }\n")
	buf.WriteString("    .summary-card.success { border-left-color: #10b981; }\n")
	buf.WriteString("    .summary-card.failed { border-left-color: #ef4444; }\n")
	buf.WriteString("    .summary-card.skipped { border-left-color: #f59e0b; }\n")
	buf.WriteString("    .summary-card.total { border-left-color: #667eea; }\n")
	buf.WriteString("    .summary-label { display: block; color: #666; font-size: 0.9em; font-weight: 600; margin-bottom: 8px; text-transform: uppercase; letter-spacing: 0.5px; }\n")
	buf.WriteString("    .summary-value { display: block; font-size: 2.5em; font-weight: 700; }\n")
	buf.WriteString("    .experiments { background: white; border-radius: 8px; overflow: hidden; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }\n")
	buf.WriteString("    .experiments h2 { padding: 20px; background: #f9fafb; border-bottom: 1px solid #e5e7eb; font-size: 1.3em; color: #1f2937; }\n")
	buf.WriteString("    table { width: 100%; border-collapse: collapse; }\n")
	buf.WriteString("    thead { background: #f9fafb; border-bottom: 2px solid #e5e7eb; }\n")
	buf.WriteString("    th { padding: 15px; text-align: left; font-weight: 700; color: #374151; font-size: 0.9em; text-transform: uppercase; letter-spacing: 0.5px; }\n")
	buf.WriteString("    td { padding: 15px; border-bottom: 1px solid #e5e7eb; }\n")
	buf.WriteString("    tbody tr:hover { background: #f9fafb; }\n")
	buf.WriteString("    .status-badge { display: inline-block; padding: 6px 12px; border-radius: 20px; font-size: 0.85em; font-weight: 600; text-transform: uppercase; letter-spacing: 0.5px; }\n")
	buf.WriteString("    .status-success { background: #d1fae5; color: #047857; }\n")
	buf.WriteString("    .status-failed { background: #fee2e2; color: #dc2626; }\n")
	buf.WriteString("    .status-skipped { background: #fef3c7; color: #b45309; }\n")
	buf.WriteString("    .error-text { color: #dc2626; font-family: 'Monaco', 'Menlo', 'Courier New', monospace; font-size: 0.85em; padding: 8px; background: #fef2f2; border-radius: 4px; word-break: break-word; }\n")
	buf.WriteString("    .details-text { color: #6b7280; font-size: 0.9em; font-style: italic; }\n")
	buf.WriteString("    footer { margin-top: 40px; text-align: center; color: #6b7280; font-size: 0.85em; }\n")
	buf.WriteString("    .dry-run-notice { background: #fef3c7; border-left: 4px solid #f59e0b; padding: 15px; border-radius: 4px; margin-bottom: 20px; color: #92400e; font-weight: 500; }\n")
	buf.WriteString("  </style>\n")
	buf.WriteString("</head>\n")
	buf.WriteString("<body>\n")
	buf.WriteString("  <div class=\"container\">\n")

	// Write header
	buf.WriteString("    <header>\n")
	buf.WriteString("      <h1>🎯 YACMO Chaos Report</h1>\n")
	buf.WriteString("      <div class=\"header-meta\">\n")
	buf.WriteString(fmt.Sprintf("        <div class=\"meta-item\"><span class=\"meta-label\">Generated</span><span class=\"meta-value\">%s</span></div>\n",
		report.GeneratedAt.Format("2006-01-02 15:04:05")))
	buf.WriteString(fmt.Sprintf("        <div class=\"meta-item\"><span class=\"meta-label\">Total Duration</span><span class=\"meta-value\">%s</span></div>\n",
		report.Duration))
	buf.WriteString(fmt.Sprintf("        <div class=\"meta-item\"><span class=\"meta-label\">Version</span><span class=\"meta-value\">%s</span></div>\n",
		report.Version))
	buf.WriteString("      </div>\n")
	buf.WriteString("    </header>\n")

	// Write dry-run notice if applicable
	if report.DryRun {
		buf.WriteString("    <div class=\"dry-run-notice\">⚠️ This was a DRY RUN - no actual chaos was injected</div>\n")
	}

	// Write summary cards
	buf.WriteString("    <div class=\"summary\">\n")
	buf.WriteString(fmt.Sprintf("      <div class=\"summary-card total\"><span class=\"summary-label\">Total</span><span class=\"summary-value\">%d</span></div>\n",
		report.Summary.Total))
	buf.WriteString(fmt.Sprintf("      <div class=\"summary-card success\"><span class=\"summary-label\">Succeeded</span><span class=\"summary-value\">%d</span></div>\n",
		report.Summary.Succeeded))
	buf.WriteString(fmt.Sprintf("      <div class=\"summary-card failed\"><span class=\"summary-label\">Failed</span><span class=\"summary-value\">%d</span></div>\n",
		report.Summary.Failed))
	buf.WriteString(fmt.Sprintf("      <div class=\"summary-card skipped\"><span class=\"summary-label\">Skipped</span><span class=\"summary-value\">%d</span></div>\n",
		report.Summary.Skipped))
	buf.WriteString("    </div>\n")

	// Write experiments table
	buf.WriteString("    <div class=\"experiments\">\n")
	buf.WriteString("      <h2>📊 Experiment Details</h2>\n")
	buf.WriteString("      <table>\n")
	buf.WriteString("        <thead>\n")
	buf.WriteString("          <tr>\n")
	buf.WriteString("            <th>Name</th>\n")
	buf.WriteString("            <th>Status</th>\n")
	buf.WriteString("            <th>Started At</th>\n")
	buf.WriteString("            <th>Duration</th>\n")
	buf.WriteString("            <th>Error</th>\n")
	buf.WriteString("            <th>Details</th>\n")
	buf.WriteString("          </tr>\n")
	buf.WriteString("        </thead>\n")
	buf.WriteString("        <tbody>\n")

	// Write rows
	for _, e := range report.Experiments {
		buf.WriteString("          <tr>\n")
		buf.WriteString(fmt.Sprintf("            <td><strong>%s</strong></td>\n", htmlEscape(e.Name)))

		// Status badge
		statusClass := "status-" + e.Status
		buf.WriteString(fmt.Sprintf("            <td><span class=\"status-badge %s\">%s</span></td>\n",
			statusClass, htmlEscape(e.Status)))

		buf.WriteString(fmt.Sprintf("            <td>%s</td>\n", e.StartedAt.Format("2006-01-02 15:04:05")))
		buf.WriteString(fmt.Sprintf("            <td>%s</td>\n", htmlEscape(e.Duration)))

		// Error cell
		if e.Error != "" {
			buf.WriteString(fmt.Sprintf("            <td><div class=\"error-text\">%s</div></td>\n", htmlEscape(e.Error)))
		} else {
			buf.WriteString("            <td></td>\n")
		}

		// Details cell
		if e.Details != "" {
			buf.WriteString(fmt.Sprintf("            <td><div class=\"details-text\">%s</div></td>\n", htmlEscape(e.Details)))
		} else {
			buf.WriteString("            <td></td>\n")
		}
		buf.WriteString("          </tr>\n")
	}

	buf.WriteString("        </tbody>\n")
	buf.WriteString("      </table>\n")
	buf.WriteString("    </div>\n")

	// Write footer
	buf.WriteString("    <footer>\n")
	buf.WriteString("      <p>Generated by YACMO - Yet Another Chaos Monkey</p>\n")
	buf.WriteString("    </footer>\n")

	buf.WriteString("  </div>\n")
	buf.WriteString("</body>\n")
	buf.WriteString("</html>\n")

	return buf.Bytes(), nil
}

// htmlEscape escapes basic HTML characters to prevent injection
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#x27;")
	return s
}

func writeJson(report *Report) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return make([]byte, 0), fmt.Errorf("marshalling report: %w", err)
	}
	return data, nil
}
