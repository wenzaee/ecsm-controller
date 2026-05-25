package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/config"
	"github.com/wenzaee/ecsm-controller/pkg/desiredclient"
	desiredvsoa "github.com/wenzaee/ecsm-controller/pkg/vsoa"
)

func main() {
	configPath := flag.String("config", "configs/demo.yaml", "YAML config file path")
	addr := flag.String("addr", "192.168.50.82:13447", "VSOA server address")
	password := flag.String("password", "", "VSOA password")
	action := flag.String("action", "healthz", "action: healthz, update, query, delete, list")
	serviceName := flag.String("service", "", "service name")
	desiredAction := flag.String("desired-action", string(desiredclient.ActionStart), "desired action")
	replicas := flag.Int("replicas", 1, "desired replicas")
	payload := flag.String("payload", "", "raw JSON request payload")
	timeout := flag.Duration("timeout", 5*time.Second, "request timeout")
	flag.Parse()

	actionName := strings.ToLower(strings.TrimSpace(*action))
	if !isSupportedAction(actionName) {
		log.Fatalf("不支持的 action: %s", *action)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}

	address := firstNonEmpty(*addr, cfg.VSOA.ListenAddr, ":18082")
	pass := firstNonEmpty(*password, cfg.VSOA.Password)

	c, err := desiredclient.New(desiredclient.Option{
		Address:  address,
		Password: pass,
	})
	if err != nil {
		log.Fatalf("创建 VSOA client 失败: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("关闭 VSOA client 失败: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var resp any
	switch actionName {
	case "healthz":
		resp, err = c.Healthz(ctx)
	case "update":
		req, buildErr := buildUpdateRequest(*payload, *serviceName, *desiredAction, *replicas)
		if buildErr != nil {
			log.Fatalf("构造 update 请求失败: %v", buildErr)
		}
		resp, err = c.UpdateDesiredRequest(ctx, req)
	case "query":
		resp, err = c.QueryDesired(ctx, *serviceName)
	case "delete":
		resp, err = c.DeleteDesired(ctx, *serviceName)
	case "list":
		resp, err = c.ListDesired(ctx)
	}
	if err != nil {
		log.Fatalf("调用 VSOA 失败: %v", err)
	}

	printJSON(resp)
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

func printJSON(value any) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatalf("JSON 编码失败: %v", err)
	}
	fmt.Println(string(payload))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
