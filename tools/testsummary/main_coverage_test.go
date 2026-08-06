package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunCLIExitCodes(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "events.json")
	writeFile(t, input, `{"Action":"pass","Package":"example/pkg","Test":"TestA","Elapsed":0.1}`+"\n")
	output := filepath.Join(dir, "summary.md")

	if code := runCLI([]string{"-unknown-flag"}); code != 2 {
		t.Fatalf("runCLI() with bad flag = %d, want 2", code)
	}
	if code := runCLI(nil); code != 2 {
		t.Fatalf("runCLI() without flags = %d, want 2", code)
	}
	if code := runCLI([]string{"--input", input}); code != 2 {
		t.Fatalf("runCLI() without output = %d, want 2", code)
	}
	if code := runCLI([]string{"--input", filepath.Join(dir, "missing.json"), "--output", output}); code != 1 {
		t.Fatalf("runCLI() with missing input = %d, want 1", code)
	}
	if code := runCLI([]string{"--input", input, "--output", filepath.Join(dir, "blocked", "summary.md")}); code != 1 {
		t.Fatalf("runCLI() with unwritable output = %d, want 1", code)
	}
	if code := runCLI([]string{"--input", input, "--output", output, "--exit-code", "0"}); code != 0 {
		t.Fatalf("runCLI() success = %d, want 0", code)
	}
	data, err := os.ReadFile(output)
	if err != nil || !strings.Contains(string(data), "Backend Test Report") {
		t.Fatalf("report file = (%q, %v)", string(data), err)
	}
}

func TestBuildReportReportsMissingInput(t *testing.T) {
	if _, err := buildReport(filepath.Join(t.TempDir(), "missing.json"), "", 0); err == nil {
		t.Fatal("buildReport() accepted missing input file")
	}
}

func TestBuildReportReportsOversizedLine(t *testing.T) {
	input := filepath.Join(t.TempDir(), "events.json")
	writeFile(t, input, strings.Repeat("a", 3*1024*1024)+"\n")
	if _, err := buildReport(input, "", 0); err == nil {
		t.Fatal("buildReport() accepted oversized line")
	}
}

func TestBuildReportCoversAllEventKinds(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "events.json")
	events := strings.Join([]string{
		"not-json line",
		`{"Action":"run","Package":"example/pkg-a","Test":"TestPass"}`,
		`{"Action":"output","Package":"example/pkg-a","Test":"TestPass","Output":"some output"}`,
		`{"Action":"pass","Package":"example/pkg-a","Test":"TestPass","Elapsed":0.2}`,
		`{"Action":"run","Package":"example/pkg-a","Test":"TestStartedOnly"}`,
		`{"Action":"run","Package":"example/pkg-b","Test":"TestFail"}`,
		`{"Action":"output","Package":"example/pkg-b","Test":"TestFail","Output":"boom details"}`,
		`{"Action":"fail","Package":"example/pkg-b","Test":"TestFail","Elapsed":0.3}`,
		`{"Action":"run","Package":"example/pkg-b","Test":"TestFailSilent"}`,
		`{"Action":"fail","Package":"example/pkg-b","Test":"TestFailSilent","Elapsed":0.1}`,
		`{"Action":"run","Package":"example/pkg-b","Test":"TestSkip"}`,
		`{"Action":"skip","Package":"example/pkg-b","Test":"TestSkip","Elapsed":0}`,
	}, "\n")
	writeFile(t, input, events)
	coverage := filepath.Join(dir, "coverage.txt")
	writeFile(t, coverage, "no total line here\n")

	report, err := buildReport(input, coverage, 1)
	if err != nil {
		t.Fatalf("buildReport() error = %v", err)
	}
	for _, want := range []string{
		"执行结论：失败",
		"| 失败 | 2 |",
		"| 跳过 | 1 |",
		"| 语句覆盖率 | 未生成 |",
		"| 测试命令退出码 | `1` |",
		"`example/pkg-a`",
		"`example/pkg-b`",
		"boom details",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not contain %q", want)
		}
	}
}

func TestLessTestResultOrdersByPackageThenName(t *testing.T) {
	a := &testResult{packageName: "pkg", name: "TestA"}
	b := &testResult{packageName: "pkg", name: "TestB"}
	c := &testResult{packageName: "other", name: "TestZ"}
	if !lessTestResult(a, b) || lessTestResult(b, a) {
		t.Fatal("lessTestResult() did not order by test name within one package")
	}
	if !lessTestResult(c, a) || lessTestResult(a, c) {
		t.Fatal("lessTestResult() did not order by package name")
	}
}

func TestMainInvokesRunCLI(t *testing.T) {
	var code int
	originalExit := osExit
	originalArgs := os.Args
	osExit = func(c int) { code = c; panic("exit") }
	os.Args = []string{"testsummary"}
	defer func() {
		recover()
		osExit = originalExit
		os.Args = originalArgs
		if code != 2 {
			t.Fatalf("main() exit code = %d, want 2", code)
		}
	}()
	main()
}
