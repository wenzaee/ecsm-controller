// Command testsummary converts Go's JSON test events into a readable GitHub
// Actions job summary. It intentionally has no third-party dependencies so it
// can run in the same CI environment as the application tests.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

type testResult struct {
	packageName string
	name        string
	action      string
	elapsed     float64
	output      strings.Builder
}

func main() {
	input := flag.String("input", "", "path to go test -json output")
	coverage := flag.String("coverage", "", "optional go tool cover -func output")
	output := flag.String("output", "", "path for the Markdown report")
	exitCode := flag.Int("exit-code", 0, "exit code returned by go test")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: testsummary --input test-results.json [--coverage coverage.txt] --output test-summary.md")
		os.Exit(2)
	}

	report, err := buildReport(*input, *coverage, *exitCode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildReport(input, coverage string, exitCode int) (string, error) {
	file, err := os.Open(input)
	if err != nil {
		return "", fmt.Errorf("open test event file: %w", err)
	}
	defer file.Close()

	results := make(map[string]*testResult)
	packages := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 2*1024*1024)
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // Keep a partial report useful if a tool writes a non-JSON line.
		}
		if event.Package != "" {
			packages[event.Package] = struct{}{}
		}
		if event.Test == "" {
			continue
		}
		key := event.Package + "::" + event.Test
		result, ok := results[key]
		if !ok {
			result = &testResult{packageName: event.Package, name: event.Test}
			results[key] = result
		}
		if event.Action == "output" {
			result.output.WriteString(event.Output)
			continue
		}
		if event.Action == "pass" || event.Action == "fail" || event.Action == "skip" {
			result.action = event.Action
			result.elapsed = event.Elapsed
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read test event file: %w", err)
	}

	byAction := map[string][]*testResult{"pass": {}, "fail": {}, "skip": {}}
	var totalElapsed float64
	for _, result := range results {
		if result.action == "" {
			continue
		}
		byAction[result.action] = append(byAction[result.action], result)
		totalElapsed += result.elapsed
	}
	for _, list := range byAction {
		sort.Slice(list, func(i, j int) bool {
			if list[i].packageName == list[j].packageName {
				return list[i].name < list[j].name
			}
			return list[i].packageName < list[j].packageName
		})
	}

	coverageValue := "Not available"
	if coverage != "" {
		if data, err := os.ReadFile(coverage); err == nil {
			re := regexp.MustCompile(`total:\s+\(statements\)\s+([0-9.]+%)`)
			if match := re.FindStringSubmatch(string(data)); len(match) == 2 {
				coverageValue = match[1]
			}
		}
	}

	total := len(byAction["pass"]) + len(byAction["fail"]) + len(byAction["skip"])
	status, icon := "Passed", "✅"
	if len(byAction["fail"]) > 0 || exitCode != 0 {
		status, icon = "Failed", "❌"
	}

	var report strings.Builder
	fmt.Fprintf(&report, "# Backend test report %s\n\n", icon)
	fmt.Fprintf(&report, "**Status:** %s\n\n", status)
	report.WriteString("| Metric | Result |\n| --- | ---: |\n")
	fmt.Fprintf(&report, "| Test cases (including subtests) | %d |\n", total)
	fmt.Fprintf(&report, "| Passed | %d |\n", len(byAction["pass"]))
	fmt.Fprintf(&report, "| Failed | %d |\n", len(byAction["fail"]))
	fmt.Fprintf(&report, "| Skipped | %d |\n", len(byAction["skip"]))
	fmt.Fprintf(&report, "| Packages | %d |\n", len(packages))
	fmt.Fprintf(&report, "| Test duration | %.2fs |\n", totalElapsed)
	fmt.Fprintf(&report, "| Statement coverage | %s |\n", coverageValue)
	if exitCode != 0 {
		fmt.Fprintf(&report, "| Test command exit code | `%s` |\n", strconv.Itoa(exitCode))
	}

	writeResults(&report, "Failed tests", byAction["fail"], true)
	writeResults(&report, "Passed tests", byAction["pass"], false)
	writeResults(&report, "Skipped tests", byAction["skip"], false)
	report.WriteString("\nDownload the **backend-test-report** artifact for the raw JSON events and interactive HTML coverage report.\n")
	return report.String(), nil
}

func writeResults(report *strings.Builder, title string, results []*testResult, includeOutput bool) {
	if len(results) == 0 {
		return
	}
	fmt.Fprintf(report, "\n<details%s>\n<summary>%s (%d)</summary>\n\n", map[bool]string{true: " open", false: ""}[includeOutput], title, len(results))
	report.WriteString("| Package | Test | Duration |\n| --- | --- | ---: |\n")
	for _, result := range results {
		fmt.Fprintf(report, "| `%s` | `%s` | %.3fs |\n", result.packageName, result.name, result.elapsed)
	}
	if includeOutput {
		for _, result := range results {
			if output := strings.TrimSpace(result.output.String()); output != "" {
				fmt.Fprintf(report, "\n**`%s` output**\n\n```text\n%s\n```\n", result.name, output)
			}
		}
	}
	report.WriteString("\n</details>\n")
}
