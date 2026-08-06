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

type packageStats struct {
	name     string
	passed   int
	failed   int
	skipped  int
	duration float64
}

// osExit 便于测试拦截进程退出。
var osExit = os.Exit

func main() {
	osExit(runCLI(os.Args[1:]))
}

// runCLI 执行测试摘要生成流程，返回进程退出码。
func runCLI(args []string) int {
	flags := flag.NewFlagSet("testsummary", flag.ContinueOnError)
	input := flags.String("input", "", "path to go test -json output")
	coverage := flags.String("coverage", "", "optional go tool cover -func output")
	output := flags.String("output", "", "path for the Markdown report")
	exitCode := flags.Int("exit-code", 0, "exit code returned by go test")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: testsummary --input test-results.json [--coverage coverage.txt] --output test-summary.md")
		return 2
	}

	report, err := buildReport(*input, *coverage, *exitCode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(*output, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
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
			return lessTestResult(list[i], list[j])
		})
	}

	coverageValue := "未生成"
	if coverage != "" {
		if data, err := os.ReadFile(coverage); err == nil {
			re := regexp.MustCompile(`total:\s+\(statements\)\s+([0-9.]+%)`)
			if match := re.FindStringSubmatch(string(data)); len(match) == 2 {
				coverageValue = match[1]
			}
		}
	}

	total := len(byAction["pass"]) + len(byAction["fail"]) + len(byAction["skip"])
	status, icon := "通过", "✅"
	if len(byAction["fail"]) > 0 || exitCode != 0 {
		status, icon = "失败", "❌"
	}

	packageSummary := summarizePackages(byAction)
	packagesWithoutTests := findPackagesWithoutTests(packages, packageSummary)
	var report strings.Builder
	fmt.Fprintf(&report, "# Backend Test Report %s\n\n", icon)
	fmt.Fprintf(&report, "**执行结论：%s**\n\n", status)
	if status == "通过" {
		report.WriteString("本次后端自动化测试全部通过，未发现失败或跳过的测试用例。\n\n")
	} else {
		fmt.Fprintf(&report, "本次后端自动化测试发现 **%d** 个失败测试节点，请优先展开下方“失败用例详情”定位问题。\n\n", len(byAction["fail"]))
	}
	report.WriteString("## 执行概览\n\n| 指标 | 结果 |\n| --- | ---: |\n")
	fmt.Fprintf(&report, "| 测试节点（包含父测试和子测试） | %d |\n", total)
	fmt.Fprintf(&report, "| 通过 | %d |\n", len(byAction["pass"]))
	fmt.Fprintf(&report, "| 失败 | %d |\n", len(byAction["fail"]))
	fmt.Fprintf(&report, "| 跳过 | %d |\n", len(byAction["skip"]))
	fmt.Fprintf(&report, "| 已扫描代码包（含无测试包） | %d |\n", len(packages))
	fmt.Fprintf(&report, "| 测试执行耗时 | %.2f 秒 |\n", totalElapsed)
	fmt.Fprintf(&report, "| 语句覆盖率 | %s |\n", coverageValue)
	if exitCode != 0 {
		fmt.Fprintf(&report, "| 测试命令退出码 | `%s` |\n", strconv.Itoa(exitCode))
	}

	report.WriteString("\n## 模块汇总\n\n| 代码包 | 通过 | 失败 | 跳过 | 耗时（秒） |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, item := range packageSummary {
		fmt.Fprintf(&report, "| `%s` | %d | %d | %d | %.3f |\n", item.name, item.passed, item.failed, item.skipped, item.duration)
	}
	if len(packagesWithoutTests) > 0 {
		report.WriteString("\n## 尚未包含自动化用例的代码包\n\n")
		report.WriteString("以下代码包已被测试命令扫描，但目前没有可执行的自动化测试用例：\n\n")
		for _, packageName := range packagesWithoutTests {
			fmt.Fprintf(&report, "- `%s`\n", packageName)
		}
	}

	report.WriteString("\n## 用例详情\n")
	writeResults(&report, "失败用例详情", byAction["fail"], true)
	writeResults(&report, "通过用例详情", byAction["pass"], false)
	writeResults(&report, "跳过用例详情", byAction["skip"], false)
	report.WriteString("\n可下载 **backend-test-report** 构件，查看原始 JSON 测试事件、此 Markdown 摘要和可交互的 HTML 覆盖率报告。\n")
	return report.String(), nil
}

// lessTestResult 按代码包名和用例名排序测试结果。
func lessTestResult(a, b *testResult) bool {
	if a.packageName == b.packageName {
		return a.name < b.name
	}
	return a.packageName < b.packageName
}

func summarizePackages(byAction map[string][]*testResult) []packageStats {
	stats := make(map[string]*packageStats)
	for action, results := range byAction {
		for _, result := range results {
			item, ok := stats[result.packageName]
			if !ok {
				item = &packageStats{name: result.packageName}
				stats[result.packageName] = item
			}
			switch action {
			case "pass":
				item.passed++
			case "fail":
				item.failed++
			case "skip":
				item.skipped++
			}
			item.duration += result.elapsed
		}
	}

	items := make([]packageStats, 0, len(stats))
	for _, item := range stats {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	return items
}

func findPackagesWithoutTests(packages map[string]struct{}, summary []packageStats) []string {
	withTests := make(map[string]struct{}, len(summary))
	for _, item := range summary {
		withTests[item.name] = struct{}{}
	}
	withoutTests := make([]string, 0, len(packages)-len(summary))
	for packageName := range packages {
		if _, ok := withTests[packageName]; !ok {
			withoutTests = append(withoutTests, packageName)
		}
	}
	sort.Strings(withoutTests)
	return withoutTests
}

func writeResults(report *strings.Builder, title string, results []*testResult, includeOutput bool) {
	if len(results) == 0 {
		return
	}
	fmt.Fprintf(report, "\n<details%s>\n<summary>%s (%d)</summary>\n\n", map[bool]string{true: " open", false: ""}[includeOutput], title, len(results))
	report.WriteString("| 代码包 | 测试用例 | 耗时（秒） |\n| --- | --- | ---: |\n")
	for _, result := range results {
		fmt.Fprintf(report, "| `%s` | `%s` | %.3f 秒 |\n", result.packageName, result.name, result.elapsed)
	}
	if includeOutput {
		for _, result := range results {
			if output := strings.TrimSpace(result.output.String()); output != "" {
				fmt.Fprintf(report, "\n**`%s` 的输出**\n\n```text\n%s\n```\n", result.name, output)
			}
		}
	}
	report.WriteString("\n</details>\n")
}
