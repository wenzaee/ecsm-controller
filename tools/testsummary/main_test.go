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
		`{"Action":"start","Package":"example/no-tests"}`,
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
	for _, want := range []string{"执行结论：通过", "| 通过 | 1 |", "| 语句覆盖率 | 82.5% |", "`TestPass`", "尚未包含自动化用例的代码包"} {
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
	if !strings.Contains(report, "执行结论：失败") || !strings.Contains(report, "| 测试命令退出码 | `1` |") {
		t.Fatalf("expected failed report, got:\n%s", report)
	}
}
