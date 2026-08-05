package main

import (
	"testing"

	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

func TestCLIRequestHelpers(t *testing.T) {
	if !isSupportedAction("update") || isSupportedAction("restart") {
		t.Fatal("isSupportedAction() returned unexpected result")
	}
	if got := firstNonEmpty("", "  ", "value"); got != "  " {
		t.Fatalf("firstNonEmpty() = %q", got)
	}
	req, err := buildUpdateRequest("", " api@1.0.0 ", "start", 2)
	if err != nil || req.ServiceName != "api@1.0.0" || req.Action != registry.DesiredActionStart || req.Replicas == nil || *req.Replicas != 2 {
		t.Fatalf("buildUpdateRequest() = (%+v, %v)", req, err)
	}
	if _, err := buildUpdateRequest("", "", "start", 1); err == nil {
		t.Fatal("buildUpdateRequest() accepted empty service")
	}
	req, err = buildUpdateRequest(`{"service_name":"api@2.0.0","action":"stop","replicas":0}`, "", "", 0)
	if err != nil || req.Action != registry.DesiredActionStop {
		t.Fatalf("raw buildUpdateRequest() = (%+v, %v)", req, err)
	}
}
