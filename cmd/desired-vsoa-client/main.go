package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/config"
	"github.com/wenzaee/ecsm-controller/pkg/desiredclient"
	desiredvsoa "github.com/wenzaee/ecsm-controller/pkg/vsoa"
)

// osExit 便于测试拦截进程退出。
var osExit = os.Exit

// newClient 便于测试注入自定义 Desired State 客户端。
var newClient = desiredclient.New

func main() {
	osExit(runCLI(context.Background(), os.Args[1:], os.Stdout))
}

// runCLI 执行 desired state VSOA 客户端命令行流程，返回进程退出码。
func runCLI(ctx context.Context, args []string, out io.Writer) int {
	flags := flag.NewFlagSet("desired-vsoa-client", flag.ContinueOnError)
	configPath := flags.String("config", "configs/demo.yaml", "YAML config file path")
	addr := flags.String("addr", "192.168.50.82:13447", "VSOA server address")
	password := flags.String("password", "", "VSOA password")
	action := flags.String("action", "healthz", "action: healthz, update, query, delete, list")
	serviceName := flags.String("service", "", "service name")
	desiredAction := flags.String("desired-action", string(desiredclient.ActionStart), "desired action")
	replicas := flags.Int("replicas", 1, "desired replicas")
	payload := flags.String("payload", "", "raw JSON request payload")
	timeout := flags.Duration("timeout", 5*time.Second, "request timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	actionName := strings.ToLower(strings.TrimSpace(*action))
	if !isSupportedAction(actionName) {
		log.Printf("不支持的 action: %s", *action)
		return 1
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("读取配置失败: %v", err)
		return 1
	}

	address := firstNonEmpty(*addr, cfg.VSOA.ListenAddr, ":18082")
	pass := firstNonEmpty(*password, cfg.VSOA.Password)

	c, err := newClient(desiredclient.Option{
		Address:  address,
		Password: pass,
	})
	if err != nil {
		log.Printf("创建 VSOA client 失败: %v", err)
		return 1
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("关闭 VSOA client 失败: %v", err)
		}
	}()

	callCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	var resp any
	switch actionName {
	case "healthz":
		resp, err = c.Healthz(callCtx)
	case "update":
		req, buildErr := buildUpdateRequest(*payload, *serviceName, *desiredAction, *replicas)
		if buildErr != nil {
			log.Printf("构造 update 请求失败: %v", buildErr)
			return 1
		}
		resp, err = c.UpdateDesiredRequest(callCtx, req)
	case "query":
		resp, err = c.QueryDesired(callCtx, *serviceName)
	case "delete":
		resp, err = c.DeleteDesired(callCtx, *serviceName)
	case "list":
		resp, err = c.ListDesired(callCtx)
	}
	if err != nil {
		log.Printf("调用 VSOA 失败: %v", err)
		return 1
	}

	if err := printJSON(out, resp); err != nil {
		log.Printf("JSON 编码失败: %v", err)
		return 1
	}
	return 0
}

func buildUpdateRequest(rawPayload, serviceName, action string, replicas int) (desiredvsoa.UpdateDesiredRequest, error) {
	if strings.TrimSpace(rawPayload) != "" {
		var req desiredvsoa.UpdateDesiredRequest
		if err := json.Unmarshal([]byte(rawPayload), &req); err != nil {
			return req, fmt.Errorf("decode payload: %w", err)
		}
		return req, nil
	}

	if strings.TrimSpace(serviceName) == "" {
		return desiredvsoa.UpdateDesiredRequest{}, fmt.Errorf("service name is required")
	}

	return desiredclient.NewUpdateRequest(strings.TrimSpace(serviceName), desiredclient.Action(strings.TrimSpace(action)), replicas)
}

func isSupportedAction(action string) bool {
	switch action {
	case "healthz", "update", "query", "delete", "list":
		return true
	default:
		return false
	}
}

func printJSON(out io.Writer, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(payload))
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
