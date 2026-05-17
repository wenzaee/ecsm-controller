package ecsmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

// Client 抽象 Reconciler 需要的 ECSM 能力：
// 读取实际状态，以及根据 diff 执行实际 ECSM 操作。
type Client interface {
	CollectServices(ctx context.Context) (*ServicePage, error)
	Apply(ctx context.Context, op Operation) error
}

// Operation 是 Reconciler 计算出的 ECSM 操作请求。
type Operation struct {
	ServiceName string
	Action      string
	Desired     registry.DesiredState
	Actual      *ServiceInfo
}

// ActualCollector 是读取 ECSM 实际状态的最小接口。
type ActualCollector interface {
	CollectServices(ctx context.Context) (*ServicePage, error)
}

// LogClient 是当前阶段使用的 ECSM client。
// 它会委托 collector 读取实际状态，但 Apply 只输出日志，不真正调用 ECSM 控制接口。
type LogClient struct {
	collector ActualCollector
}

// NewLogClient 创建只打印操作的 ECSM client。
func NewLogClient(collector ActualCollector) *LogClient {
	return &LogClient{collector: collector}
}

// CollectServices 读取 ECSM 实际状态。
func (c *LogClient) CollectServices(ctx context.Context) (*ServicePage, error) {
	if c == nil || c.collector == nil {
		return nil, fmt.Errorf("ecsm actual collector is not configured")
	}
	return c.collector.CollectServices(ctx)
}

// Apply 根据 Reconciler 计算出的动作调用 ECSM。
func (c *LogClient) Apply(ctx context.Context, op Operation) error {
	log.Printf("[INFO] ecsm client apply: service=%s action=%s desired_action=%s",
		op.ServiceName, op.Action, op.Desired.Action)

	switch op.Action {
	case "none":
		return nil
	case "need_create":
		return c.createService(ctx, op)
	case "need_start":
		return c.startByID(ctx, op)
	case "need_stop":
		return c.stopByID(ctx, op)
	case "need_scale_out", "need_scale_in":
		return c.scaleByID(ctx, op)
	default:
		return fmt.Errorf("unsupported ecsm operation action: %s", op.Action)
	}
}

func (c *LogClient) createService(ctx context.Context, op Operation) error {
	collector, ok := c.collector.(*Collector)
	if !ok || collector == nil {
		return fmt.Errorf("ecsm collector does not support write operations")
	}

	serviceName := strings.TrimSpace(op.ServiceName)
	if serviceName == "" {
		serviceName = strings.TrimSpace(op.Desired.ServiceName)
	}
	if serviceName == "" {
		return fmt.Errorf("service name is required when creating service")
	}

	imageRef, err := normalizeCreateImageRef(serviceName)
	if err != nil {
		return err
	}

	imageConfig, err := collector.getImageConfig(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("get image config for %s: %w", imageRef, err)
	}
	imageConfig = ensureDefaultImageConfig(imageConfig)

	factor := 1
	if op.Desired.Replicas > 0 {
		factor = op.Desired.Replicas
	}

	req := CreateServiceRequest{
		Name: serviceName,
		Image: CreateImageSpec{
			Ref:         imageRef,
			Action:      "run",
			PullPolicy:  "IfNotPresent",
			AutoUpgrade: "Never",
			Config:      imageConfig,
			VSOA: &VSOASpec{
				Port:              0,
				Password:          "123445",
				HealthPath:        "/health",
				HealthTimeout:     3000,
				HealthRetries:     3,
				HealthStartPeriod: 5000,
				HealthInterval:    5000,
				LivenessProbe:     false,
			},
		},
		Node: CreateNodeSpec{
			Names: []string{"master"},
		},
		Policy: "dynamic",
		Factor: factor,
	}

	log.Printf("[INFO] ecsm client create request: service=%s image=%s factor=%d", serviceName, imageRef, factor)
	var created ServiceCreateResponse
	if err := collector.doJSONRequest(ctx, http.MethodPost, servicePath, nil, req, &created); err != nil {
		return err
	}
	log.Printf("[INFO] ecsm client create success: service=%s image=%s id=%s", serviceName, imageRef, created.ID)
	return nil
}

func (c *LogClient) startByID(ctx context.Context, op Operation) error {
	if op.Actual == nil || op.Actual.ID == "" {
		return fmt.Errorf("actual service id is required for start: service=%s", op.ServiceName)
	}

	serviceID := op.Actual.ID
	if replicasDiffer(op) {
		updatedID, err := c.updateReplicasByID(ctx, op)
		if err != nil {
			return err
		}
		if updatedID != "" {
			serviceID = updatedID
		}
	}
	return c.actionByID(ctx, serviceID, "start")
}

func (c *LogClient) stopByID(ctx context.Context, op Operation) error {
	if op.Actual == nil || op.Actual.ID == "" {
		return fmt.Errorf("actual service id is required for stop: service=%s", op.ServiceName)
	}

	serviceID := op.Actual.ID
	if replicasDiffer(op) {
		updatedID, err := c.updateReplicasByID(ctx, op)
		if err != nil {
			return err
		}
		if updatedID != "" {
			serviceID = updatedID
		}
	}
	return c.actionByID(ctx, serviceID, "stop")
}

func (c *LogClient) actionByID(ctx context.Context, serviceID, action string) error {
	collector, ok := c.collector.(*Collector)
	if !ok || collector == nil {
		return fmt.Errorf("ecsm collector does not support write operations")
	}

	var ids []string
	reqBody := serviceBatchActionRequest{IDs: []string{serviceID}}
	apiPath := path.Join(servicePath, action, "ids")
	if err := collector.doJSONRequest(ctx, http.MethodPost, apiPath, nil, reqBody, &ids); err != nil {
		return err
	}
	log.Printf("[INFO] ecsm client action success: action=%s ids=%v", action, ids)
	return nil
}

func (c *LogClient) scaleByID(ctx context.Context, op Operation) error {
	if op.Actual == nil || op.Actual.ID == "" {
		return fmt.Errorf("actual service id is required for scale: service=%s", op.ServiceName)
	}
	if op.Desired.Replicas <= 0 {
		return fmt.Errorf("desired replicas must be greater than 0 for scale: service=%s replicas=%s",
			op.ServiceName, formatReplicas(op.Desired.Replicas))
	}

	updatedID, err := c.updateReplicasByID(ctx, op)
	if err != nil {
		return err
	}
	log.Printf("[INFO] ecsm client scale success: service=%s id=%s replicas=%d updated_id=%s",
		op.ServiceName, op.Actual.ID, op.Desired.Replicas, updatedID)
	if op.Desired.Action == registry.DesiredActionStart && !isActualRunning(*op.Actual) {
		serviceID := op.Actual.ID
		if updatedID != "" {
			serviceID = updatedID
		}
		return c.actionByID(ctx, serviceID, "start")
	}
	return nil
}

func (c *LogClient) updateReplicasByID(ctx context.Context, op Operation) (string, error) {
	collector, ok := c.collector.(*Collector)
	if !ok || collector == nil {
		return "", fmt.Errorf("ecsm collector does not support write operations")
	}

	detail, err := collector.getServiceDetailByID(ctx, op.Actual.ID)
	if err != nil {
		return "", err
	}

	updateReq := UpdateServiceRequest{
		ID:     detail.ID,
		Name:   detail.Name,
		Image:  detail.Image,
		Node:   detail.Node,
		Factor: op.Desired.Replicas,
		Policy: detail.Policy,
	}
	if strings.TrimSpace(updateReq.Policy) == "" {
		updateReq.Policy = "dynamic"
	}
	if updateReq.Image.Action == "" {
		updateReq.Image.Action = "run"
	}
	if updateReq.Image.Config == nil {
		updateReq.Image.Config = defaultImageConfig()
	}

	var updated ServiceCreateResponse
	if err := collector.doJSONRequest(ctx, http.MethodPut, servicePath, nil, updateReq, &updated); err != nil {
		return "", err
	}
	log.Printf("[INFO] ecsm client replicas update success: service=%s id=%s replicas=%d updated_id=%s",
		op.ServiceName, op.Actual.ID, op.Desired.Replicas, updated.ID)
	return updated.ID, nil
}

func replicasDiffer(op Operation) bool {
	if op.Actual == nil || op.Desired.Replicas <= 0 {
		return false
	}
	if op.Actual.Factor > 0 {
		return op.Actual.Factor != op.Desired.Replicas
	}
	return actualReplicaCount(*op.Actual) != op.Desired.Replicas
}

func actualReplicaCount(actual ServiceInfo) int {
	return actual.InstanceOnline + actual.InstanceActive
}

func isActualRunning(actual ServiceInfo) bool {
	if actual.InstanceOnline > 0 {
		return true
	}
	for _, status := range actual.ContainerStatusGroup {
		if strings.EqualFold(status, "running") {
			return true
		}
	}
	return false
}

func (c *Collector) getServiceDetailByID(ctx context.Context, serviceID string) (*ServiceDetail, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("serviceID is required")
	}

	var result ServiceDetail
	apiPath := path.Join(servicePath, url.PathEscape(serviceID))
	if err := c.doJSONRequest(ctx, http.MethodGet, apiPath, nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Collector) getImageConfig(ctx context.Context, ref string) (*EcsImageConfig, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("image ref is required")
	}

	query := url.Values{}
	query.Set("ref", ref)

	responseWrapper := struct {
		Config *EcsImageConfig `json:"config"`
	}{}
	if err := c.doJSONRequest(ctx, http.MethodGet, "/api/v1/image/config", query, nil, &responseWrapper); err != nil {
		return nil, err
	}
	if responseWrapper.Config == nil {
		return nil, fmt.Errorf("image config is empty for ref %s", ref)
	}
	return responseWrapper.Config, nil
}

func (c *Collector) doJSONRequest(ctx context.Context, method, apiPath string, query url.Values, body any, out any) error {
	fullURL := c.baseURL() + apiPath
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	var cr commonResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return fmt.Errorf("decode common response: %w", err)
	}
	if cr.Status != http.StatusOK {
		return fmt.Errorf("ECSM API %s %s failed: status=%d message=%s", method, apiPath, cr.Status, cr.Message)
	}

	if out == nil || len(cr.Data) == 0 || string(cr.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(cr.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}

func (c *Collector) baseURL() string {
	u := url.URL{
		Scheme: c.cfg.Scheme,
		Host:   c.cfg.IP + ":" + strconv.Itoa(c.cfg.Port),
	}
	return u.String()
}

func formatReplicas(replicas int) string {
	return strconv.Itoa(replicas)
}

func normalizeCreateImageRef(image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", fmt.Errorf("image is required when creating service")
	}

	imagePart, imageOS, _ := strings.Cut(image, "#")
	imagePart = strings.TrimSpace(imagePart)
	imageOS = strings.TrimSpace(imageOS)
	if imageOS == "" {
		imageOS = "sylixos"
	}

	if strings.Contains(imagePart, "@") {
		return imagePart + "#" + imageOS, nil
	}

	name, version, ok := splitDockerStyleImage(imagePart)
	if !ok {
		return "", fmt.Errorf("image version is required when creating service: %s", image)
	}
	return name + "@" + version + "#" + imageOS, nil
}

func splitDockerStyleImage(image string) (string, string, bool) {
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return "", "", false
	}

	name := strings.TrimSpace(image[:lastColon])
	version := strings.TrimSpace(image[lastColon+1:])
	if name == "" || version == "" {
		return "", "", false
	}
	return name, version, true
}

type commonResponse struct {
	Status      int             `json:"status"`
	Message     string          `json:"message"`
	FieldErrors json.RawMessage `json:"fieldErrors"`
	Data        json.RawMessage `json:"data"`
}

type serviceBatchActionRequest struct {
	IDs []string `json:"ids"`
}

type CreateServiceRequest struct {
	Name   string          `json:"name"`
	Image  CreateImageSpec `json:"image"`
	Node   CreateNodeSpec  `json:"node"`
	Policy string          `json:"policy,omitempty"`
	Factor int             `json:"factor,omitempty"`
}

// ServiceDetail 是更新服务时需要保留的 ECSM 服务详情。
type ServiceDetail struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Status string          `json:"status"`
	Factor int             `json:"factor"`
	Policy string          `json:"policy"`
	Image  CreateImageSpec `json:"image"`
	Node   CreateNodeSpec  `json:"node"`
}

type UpdateServiceRequest struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Image  CreateImageSpec `json:"image"`
	Node   CreateNodeSpec  `json:"node"`
	Factor int             `json:"factor,omitempty"`
	Policy string          `json:"policy,omitempty"`
}

type ServiceCreateResponse struct {
	ID         string   `json:"id"`
	Containers []string `json:"containers"`
}

type CreateImageSpec struct {
	Ref         string          `json:"ref"`
	Action      string          `json:"action"`
	PullPolicy  string          `json:"pullPolicy,omitempty"`
	AutoUpgrade string          `json:"autoUpgrade,omitempty"`
	Config      *EcsImageConfig `json:"config"`
	VSOA        *VSOASpec       `json:"vsoa,omitempty"`
}

type CreateNodeSpec struct {
	Names []string `json:"names"`
}

type EcsImageConfig struct {
	Platform *Platform `json:"platform,omitempty"`
	Process  *Process  `json:"process,omitempty"`
	Root     *Root     `json:"root,omitempty"`
	Hostname string    `json:"hostname,omitempty"`
	Mounts   []Mount   `json:"mounts,omitempty"`
	SylixOS  *SylixOS  `json:"sylixos,omitempty"`
}

type Platform struct {
	OS   string `json:"os,omitempty"`
	Arch string `json:"arch,omitempty"`
}

type Process struct {
	Args []string `json:"args,omitempty"`
	Env  []string `json:"env,omitempty"`
	Cwd  string   `json:"cwd,omitempty"`
}

type Root struct {
	Path     string `json:"path,omitempty"`
	Readonly bool   `json:"readonly"`
}

type Mount struct {
	Destination string   `json:"destination"`
	Source      string   `json:"source"`
	Options     []string `json:"options,omitempty"`
}

type SylixOS struct {
	Devices   []Device   `json:"devices,omitempty"`
	Resources *Resources `json:"resources,omitempty"`
	Network   *Network   `json:"network,omitempty"`
	Commands  []string   `json:"commands,omitempty"`
}

type Device struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

type Resources struct {
	CPU          *CPU          `json:"cpu,omitempty"`
	Memory       *Memory       `json:"memory,omitempty"`
	Disk         *Disk         `json:"disk,omitempty"`
	KernelObject *KernelObject `json:"kernelObject,omitempty"`
}

type CPU struct {
	HighestPrio int `json:"highestPrio"`
	LowestPrio  int `json:"lowestPrio"`
	DefaultPrio int `json:"defaultPrio,omitempty"`
}

type Memory struct {
	MemoryLimitMB int `json:"memoryLimitMB"`
	KheapLimit    int `json:"kheapLimit,omitempty"`
}

type Disk struct {
	LimitMB int `json:"limitMB"`
}

type KernelObject struct {
	EventLimit      int `json:"eventLimit,omitempty"`
	EventSetLimit   int `json:"eventSetLimit,omitempty"`
	MsgQueueLimit   int `json:"msgQueueLimit,omitempty"`
	PartitionLimit  int `json:"partitionLimit,omitempty"`
	RegionLimit     int `json:"regionLimit,omitempty"`
	ThreadLimit     int `json:"threadLimit,omitempty"`
	ThreadPoolLimit int `json:"threadPoolLimit,omitempty"`
	TimerLimit      int `json:"timerLimit,omitempty"`
}

type Network struct {
	FtpdEnable    bool `json:"ftpdEnable"`
	TelnetdEnable bool `json:"telnetdEnable"`
}

type VSOASpec struct {
	Port              int    `json:"port"`
	Password          string `json:"password,omitempty"`
	HealthPath        string `json:"healthPath,omitempty"`
	HealthTimeout     int    `json:"healthTimeout,omitempty"`
	HealthRetries     int    `json:"healthRetries,omitempty"`
	HealthStartPeriod int    `json:"healthStartPeriod,omitempty"`
	HealthInterval    int    `json:"healthInterval,omitempty"`
	LivenessProbe     bool   `json:"livenessProbe,omitempty"`
}

func defaultImageConfig() *EcsImageConfig {
	return ensureDefaultImageConfig(nil)
}

func ensureDefaultImageConfig(cfg *EcsImageConfig) *EcsImageConfig {
	if cfg == nil {
		cfg = &EcsImageConfig{}
	}
	if cfg.Platform == nil {
		cfg.Platform = &Platform{}
	}
	if cfg.Process == nil {
		cfg.Process = &Process{}
	}
	if len(cfg.Process.Args) == 0 {
		cfg.Process.Args = []string{"/apps/worker1"}
	}
	if len(cfg.Process.Env) == 0 {
		cfg.Process.Env = []string{
			"PATH=/usr/bin:/bin:/usr/pkg/sbin:/sbin:/usr/local/bin",
			"LD_LIBRARY_PATH=/usr/lib:/lib:/usr/local/lib",
		}
	}
	if cfg.Root == nil {
		cfg.Root = &Root{}
	}
	if strings.TrimSpace(cfg.Hostname) == "" {
		cfg.Hostname = "sylixos_ecs"
	}
	if len(cfg.Mounts) == 0 {
		cfg.Mounts = []Mount{{
			Destination: "/etc/lic",
			Source:      "/etc/lic",
			Options:     []string{"ro"},
		}}
	}
	if cfg.SylixOS == nil {
		cfg.SylixOS = &SylixOS{}
	}
	if len(cfg.SylixOS.Devices) == 0 {
		cfg.SylixOS.Devices = []Device{
			{Path: "/dev/fb0", Access: "rw"},
			{Path: "/dev/input/xmse", Access: "rw"},
			{Path: "/dev/input/xkbd", Access: "rw"},
			{Path: "/dev/net/vnd", Access: "rw"},
		}
	}
	if cfg.SylixOS.Resources == nil {
		cfg.SylixOS.Resources = &Resources{}
	}
	if cfg.SylixOS.Resources.CPU == nil {
		cfg.SylixOS.Resources.CPU = &CPU{HighestPrio: 160, LowestPrio: 250, DefaultPrio: 200}
	} else {
		if cfg.SylixOS.Resources.CPU.HighestPrio == 0 {
			cfg.SylixOS.Resources.CPU.HighestPrio = 160
		}
		if cfg.SylixOS.Resources.CPU.LowestPrio == 0 {
			cfg.SylixOS.Resources.CPU.LowestPrio = 250
		}
		if cfg.SylixOS.Resources.CPU.DefaultPrio == 0 {
			cfg.SylixOS.Resources.CPU.DefaultPrio = 200
		}
	}
	if cfg.SylixOS.Resources.Memory == nil {
		cfg.SylixOS.Resources.Memory = &Memory{KheapLimit: 2097152, MemoryLimitMB: 2048}
	} else {
		if cfg.SylixOS.Resources.Memory.KheapLimit == 0 {
			cfg.SylixOS.Resources.Memory.KheapLimit = 2097152
		}
		cfg.SylixOS.Resources.Memory.MemoryLimitMB = 2048
	}
	if cfg.SylixOS.Resources.Disk == nil {
		cfg.SylixOS.Resources.Disk = &Disk{LimitMB: 2048}
	} else if cfg.SylixOS.Resources.Disk.LimitMB == 0 {
		cfg.SylixOS.Resources.Disk.LimitMB = 2048
	}
	if cfg.SylixOS.Resources.KernelObject == nil {
		cfg.SylixOS.Resources.KernelObject = &KernelObject{
			EventLimit:      32768,
			EventSetLimit:   500,
			MsgQueueLimit:   8192,
			PartitionLimit:  6000,
			RegionLimit:     50,
			ThreadLimit:     4096,
			ThreadPoolLimit: 100,
			TimerLimit:      64,
		}
	} else {
		if cfg.SylixOS.Resources.KernelObject.EventLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.EventLimit = 32768
		}
		if cfg.SylixOS.Resources.KernelObject.EventSetLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.EventSetLimit = 500
		}
		if cfg.SylixOS.Resources.KernelObject.MsgQueueLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.MsgQueueLimit = 8192
		}
		if cfg.SylixOS.Resources.KernelObject.PartitionLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.PartitionLimit = 6000
		}
		if cfg.SylixOS.Resources.KernelObject.RegionLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.RegionLimit = 50
		}
		if cfg.SylixOS.Resources.KernelObject.ThreadLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.ThreadLimit = 4096
		}
		if cfg.SylixOS.Resources.KernelObject.ThreadPoolLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.ThreadPoolLimit = 100
		}
		if cfg.SylixOS.Resources.KernelObject.TimerLimit == 0 {
			cfg.SylixOS.Resources.KernelObject.TimerLimit = 64
		}
	}
	if cfg.SylixOS.Network == nil {
		cfg.SylixOS.Network = &Network{FtpdEnable: true, TelnetdEnable: true}
	}
	return cfg
}
