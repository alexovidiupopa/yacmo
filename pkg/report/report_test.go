package report_test

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yacmo/pkg/logger"
	"yacmo/pkg/report"
)

func testLogger() *logger.Logger { return logger.New("error") }

func buildSampleReport(t *testing.T, format string) *report.Report {
	t.Helper()
	b := report.NewBuilder(testLogger(), false, format)
	now := time.Now()
	b.RecordExperiment("ok-exp", now, 2*time.Second, nil, "success")
	b.RecordExperiment("bad-exp", now, time.Second, errors.New("failure detail"), "error: failure detail")
	b.RecordExperiment("skipped-exp", now, 0, nil, "dry-run: skipped")
	return b.Build()
}

func TestBuilderSummaryCounts(t *testing.T) {
	r := buildSampleReport(t, "json")

	if r.Summary.Total != 3 {
		t.Errorf("Total = %d, want 3", r.Summary.Total)
	}
	if r.Summary.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", r.Summary.Succeeded)
	}
	if r.Summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", r.Summary.Failed)
	}
	if r.Summary.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", r.Summary.Skipped)
	}
	if r.Format != "json" {
		t.Errorf("Format = %q, want json", r.Format)
	}
}

func TestRecordExperimentStatusMapping(t *testing.T) {
	b := report.NewBuilder(testLogger(), true, "json")
	b.RecordExperiment("e", time.Now(), time.Second, errors.New("x"), "error: x")
	r := b.Build()
	if r.Experiments[0].Status != "failed" {
		t.Errorf("status = %q, want failed", r.Experiments[0].Status)
	}
	if r.Experiments[0].Error != "x" {
		t.Errorf("error = %q, want x", r.Experiments[0].Error)
	}
	if !r.DryRun {
		t.Error("DryRun should be propagated to the report")
	}
}

func TestWriteJSON(t *testing.T) {
	dir := t.TempDir()
	r := buildSampleReport(t, "json")

	path, err := report.Write(r, dir, testLogger())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if filepath.Ext(path) != ".json" {
		t.Errorf("path %q should have .json extension", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading report: %v", err)
	}
	var decoded report.Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if decoded.Summary.Total != 3 {
		t.Errorf("decoded Total = %d, want 3", decoded.Summary.Total)
	}
}

func TestWriteCSV(t *testing.T) {
	dir := t.TempDir()
	r := buildSampleReport(t, "csv")

	path, err := report.Write(r, dir, testLogger())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open csv: %v", err)
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("parsing CSV: %v", err)
	}
	// 1 header + 3 rows.
	if len(rows) != 4 {
		t.Fatalf("got %d CSV rows, want 4", len(rows))
	}
	wantHeader := []string{"Name", "Status", "Started At", "Duration", "Error", "Details"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, rows[0][i], h)
		}
	}
}

func TestWriteHTMLEscapesContent(t *testing.T) {
	dir := t.TempDir()
	b := report.NewBuilder(testLogger(), false, "html")
	b.RecordExperiment("<script>evil</script>", time.Now(), time.Second, nil, "success")
	r := b.Build()

	path, err := report.Write(r, dir, testLogger())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading html: %v", err)
	}
	html := string(data)
	if strings.Contains(html, "<script>evil</script>") {
		t.Error("HTML report must escape experiment names to prevent injection")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("expected escaped experiment name in HTML report")
	}
}

func TestWriteUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	r := buildSampleReport(t, "xml")
	if _, err := report.Write(r, dir, testLogger()); err == nil {
		t.Error("Write should fail for an unsupported format")
	}
}
