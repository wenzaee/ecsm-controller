package registry

import (
	"strings"
)

const (
	DesiredServicePrefix = "desired/services/"
	StatusServicePrefix  = "status/services/"
)

// DesiredServiceKey 返回服务 desired state 在存储中的 key。
func DesiredServiceKey(serviceName string) string {
	return DesiredServicePrefix + serviceName
}

// StatusServiceKey 返回服务 status 在存储中的 key。
func StatusServiceKey(serviceName string) string {
	return StatusServicePrefix + serviceName
}

// ServiceNameFromDesiredKey 从 desired state key 中解析服务名。
func ServiceNameFromDesiredKey(key string) (string, bool) {
	name, ok := strings.CutPrefix(key, DesiredServicePrefix)
	return name, ok && name != ""
}

// ServiceNameFromStatusKey 从 status key 中解析服务名。
func ServiceNameFromStatusKey(key string) (string, bool) {
	name, ok := strings.CutPrefix(key, StatusServicePrefix)
	return name, ok && name != ""
}

// ValidateServiceName 校验服务名是否符合 desired state API 约定。
func ValidateServiceName(serviceName string) error {
	return validateServiceName(serviceName)
}

func validateServiceName(serviceName string) error {
	serviceName = strings.TrimSpace(serviceName)
	name, tag, ok := strings.Cut(serviceName, "@")
	if serviceName == "" || !ok || name == "" || tag == "" || strings.Contains(tag, "@") || strings.Contains(serviceName, "/") {
		return ErrInvalidServiceName
	}
	return nil
}
