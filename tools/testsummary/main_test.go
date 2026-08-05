package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReportIncludesMetricsAndCoverage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "test-results.json")
	coverage := filepath.Join(dir, "coverage.txt")
	events := strings.Join([]string{
		`{"Action":"run","Package":"example/pkg","Test":"TestPass"}`,
		`{"Action":"pass","Package":"example/pkg","Test":"TestPass","Elapsed":0.01}`,
	}, "\n")
	if err := os.WriteFile(input, []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverage, []byte("total: (statements) 82.5%\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := buildReport(input, coverage, 0)
	if err != nil {
		t.Fatalf("buildReport() error = %v", err)
	}
	for _, want := range []string{"Status:** Passed", "| Passed | 1 |", "| Statement coverage | 82.5% |", "`TestPass`"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not contain %q:\n%s", want, report)
		}
	}
}

func TestBuildReportMarksNonZeroTestExitCodeAsFailure(t *testing.T) {
	input := filepath.Join(t.TempDir(), "test-results.json")
	if err := os.WriteFile(input, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := buildReport(input, "", 1)
	if err != nil {
		t.Fatalf("buildReport() error = %v", err)
	}
	if !strings.Contains(report, "Status:** Failed") || !strings.Contains(report, "| Test command exit code | `1` |") {
		t.Fatalf("expected failed report, got:\n%s", report)
	}
}
