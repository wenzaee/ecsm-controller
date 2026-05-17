package ecsmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/config"
)

const (
	servicePath            = "/api/v1/service"
	firstServicePageNum    = 1
	defaultServicePageSize = 50
)

// Config 是 ECSM 实际状态采集配置。
type Config struct {
	Scheme  string
	IP      string
	Port    int
	Timeout time.Duration
}

// Collector 负责从 ECSM HTTP API 采集服务实际运行状态。
type Collector struct {
	cfg        Config
	httpClient *http.Client
}

// NewCollector 创建 ECSM 实际状态采集器。
func NewCollector(cfg Config) (*Collector, error) {
	cfg = normalizeConfig(cfg)
	if cfg.IP == "" {
		return nil, fmt.Errorf("ecsm ip is required")
	}
	if cfg.Port <= 0 {
		return nil, fmt.Errorf("ecsm port is required")
	}

	return &Collector{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

// NewCollectorFromConfig 根据工程配置创建实际状态采集器。
func NewCollectorFromConfig(cfg config.ECSMConfig) (*Collector, error) {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	return NewCollector(Config{
		Scheme:  cfg.Scheme,
		IP:      cfg.IP,
		Port:    cfg.Port,
		Timeout: timeout,
	})
}

// CollectServices 采集 ECSM 当前服务实际状态列表，会自动分页拉取完整列表。
func (c *Collector) CollectServices(ctx context.Context) (*ServicePage, error) {
	var all []ServiceInfo
	total := 0

	for pageNum := firstServicePageNum; ; pageNum++ {
		page, err := c.collectServicePage(ctx, pageNum, defaultServicePageSize)
		if err != nil {
			return nil, err
		}

		if total == 0 && page.Total > 0 {
			total = page.Total
		}
		all = append(all, page.List...)

		if len(page.List) < defaultServicePageSize {
			break
		}
		if total > 0 && len(all) >= total {
			break
		}
	}

	if total == 0 {
		total = len(all)
	}
	return &ServicePage{
		List:     all,
		Total:    total,
		PageSize: defaultServicePageSize,
		PageNum:  firstServicePageNum,
	}, nil
}

func (c *Collector) collectServicePage(ctx context.Context, pageNum, pageSize int) (*ServicePage, error) {
	endpoint := c.serviceURL(pageNum, pageSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("collect actual services from %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("collect actual services http status: %s", resp.Status)
	}

	var apiResp ServiceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode actual services response: %w", err)
	}
	if apiResp.Status != http.StatusOK {
		return nil, fmt.Errorf("collect actual services api status=%d message=%s", apiResp.Status, apiResp.Message)
	}
	return &apiResp.Data, nil
}

func (c *Collector) serviceURL(pageNum, pageSize int) string {
	u := url.URL{
		Scheme: c.cfg.Scheme,
		Host:   net.JoinHostPort(c.cfg.IP, strconv.Itoa(c.cfg.Port)),
		Path:   servicePath,
	}
	values := u.Query()
	values.Set("pageNum", strconv.Itoa(pageNum))
	values.Set("pageSize", strconv.Itoa(pageSize))
	u.RawQuery = values.Encode()
	return u.String()
}

func normalizeConfig(cfg Config) Config {
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return cfg
}

// ServiceListResponse 是 ECSM 服务列表接口响应。
type ServiceListResponse struct {
	Status      int         `json:"status"`
	Message     string      `json:"message"`
	FieldErrors any         `json:"fieldErrors"`
	Data        ServicePage `json:"data"`
}

// ServicePage 是服务实际状态列表分页数据。
type ServicePage struct {
	List     []ServiceInfo `json:"list"`
	Total    int           `json:"total"`
	PageSize int           `json:"pageSize"`
	PageNum  int           `json:"pageNum"`
}

// ServiceInfo 是 ECSM 返回的服务实际运行状态。
type ServiceInfo struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	CreatedTime          string      `json:"createdTime"`
	UpdatedTime          string      `json:"updatedTime"`
	Status               string      `json:"status"`
	Factor               int         `json:"factor"`
	Policy               string      `json:"policy"`
	ImageList            []ImageInfo `json:"imageList"`
	NodeList             []NodeInfo  `json:"nodeList"`
	InstanceOnline       int         `json:"instanceOnline"`
	InstanceActive       int         `json:"instanceActive"`
	ErrorInstance        []any       `json:"errorInstance"`
	ContainerStatusGroup []string    `json:"containerStatusGroup"`
	DefaultLabels        []any       `json:"defaultLabels"`
	PathLabel            string      `json:"pathLabel"`
}

// ImageInfo 是服务实际使用的镜像信息。
type ImageInfo struct {
	Name string `json:"name"`
	Tag  string `json:"tag"`
	OS   string `json:"os"`
}

// NodeInfo 是服务实际运行节点信息。
type NodeInfo struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Address  string `json:"address"`
}
