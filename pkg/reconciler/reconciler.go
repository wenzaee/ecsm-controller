package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/wenzaee/ecsm-controller/pkg/ecsmclient"
	"github.com/wenzaee/ecsm-controller/pkg/queue"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

// DiffAction 表示 desired state 和 actual state 比对后的内部收敛动作。
// 外部 desired action 只允许 start/stop；这里的 diff 是 controller 自己计算出的执行步骤。
type DiffAction string

const (
	DiffNone         DiffAction = "none"
	DiffNeedCreate   DiffAction = "need_create"
	DiffNeedStart    DiffAction = "need_start"
	DiffNeedStop     DiffAction = "need_stop"
	DiffNeedScaleOut DiffAction = "need_scale_out"
	DiffNeedScaleIn  DiffAction = "need_scale_in"
)

// DesiredReader 是 Reconciler 读取最新期望状态所需的存储接口。
type DesiredReader interface {
	QueryDesired(ctx context.Context, serviceName string) (*registry.DesiredState, error)
}

// Reconciler 负责比较 desired state 和 ECSM actual state，并决定收敛动作。
type Reconciler struct {
	desired DesiredReader
	ecsm    ecsmclient.Client
}

// New 创建 Reconciler。
func New(desired DesiredReader, ecsm ecsmclient.Client) *Reconciler {
	return &Reconciler{
		desired: desired,
		ecsm:    ecsm,
	}
}

// Worker 负责从队列中取出服务名，并交给 Reconciler 处理。
type Worker struct {
	queue      *queue.WorkQueue
	reconciler *Reconciler
}

// NewWorker 创建 Reconciler worker。
func NewWorker(workQueue *queue.WorkQueue, reconciler *Reconciler) *Worker {
	return &Worker{
		queue:      workQueue,
		reconciler: reconciler,
	}
}

// Run 持续消费队列，直到 context 取消。
func (w *Worker) Run(ctx context.Context) {
	for {
		serviceName, ok := w.queue.Get(ctx)
		if !ok {
			return
		}

		log.Printf("[INFO] reconciler worker 取出 service=%s", serviceName)
		if err := w.reconciler.Reconcile(ctx, serviceName); err != nil {
			log.Printf("[WARN] reconciler worker 处理失败: service=%s err=%v", serviceName, err)
		}
		w.queue.Done(serviceName)
		log.Printf("[INFO] reconciler worker 处理完成: service=%s", serviceName)
	}
}

// Reconcile 处理单个服务的收敛。
func (r *Reconciler) Reconcile(ctx context.Context, serviceName string) error {
	desired, err := r.desired.QueryDesired(ctx, serviceName)
	if errors.Is(err, registry.ErrNotFound) {
		log.Printf("[INFO] reconciler 跳过: service=%s desired state 不存在", serviceName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("query desired state %s: %w", serviceName, err)
	}

	if r.ecsm == nil {
		log.Printf("[WARN] reconciler 无 ecsm client，无法比对和操作: service=%s desired_action=%s",
			serviceName, desired.Action)
		return nil
	}

	page, err := r.ecsm.CollectServices(ctx)
	if err != nil {
		return fmt.Errorf("collect actual state: %w", err)
	}

	actual, found := FindActualService(page.List, serviceName)
	diff := CompareDesiredActual(desired, actual, found)
	logDiff(serviceName, desired, actual, found, diff)
	if diff == DiffNone {
		return nil
	}

	op := ecsmclient.Operation{
		ServiceName: serviceName,
		Action:      string(diff),
		Desired:     *desired,
	}
	if found {
		op.Actual = &actual
	}
	if err := r.ecsm.Apply(ctx, op); err != nil {
		return fmt.Errorf("apply ecsm operation %s for %s: %w", diff, serviceName, err)
	}
	return nil
}

// FindActualService 从 ECSM 服务列表中查找指定服务。
func FindActualService(services []ecsmclient.ServiceInfo, serviceName string) (ecsmclient.ServiceInfo, bool) {
	for _, service := range services {
		if service.Name == serviceName {
			return service, true
		}
	}
	return ecsmclient.ServiceInfo{}, false
}

// CompareDesiredActual 根据 desired state 和 actual state 计算内部收敛动作。
// desired.Action 只处理 start/stop；其他值不执行，实际写入时 registry 会拒绝。
func CompareDesiredActual(desired *registry.DesiredState, actual ecsmclient.ServiceInfo, found bool) DiffAction {
	if desired == nil {
		return DiffNone
	}

	switch desired.Action {
	case registry.DesiredActionStart:
		if !found {
			return DiffNeedCreate
		}
		actualReplicas := actualReplicaCount(actual)
		if desired.Replicas <= 0 {
			if !isActualRunning(actual) {
				return DiffNeedStart
			}
			return DiffNone
		}
		if actualReplicas < desired.Replicas {
			return DiffNeedScaleOut
		}
		if actualReplicas > desired.Replicas {
			return DiffNeedScaleIn
		}
		if !isActualRunning(actual) {
			return DiffNeedStart
		}
		return DiffNone
	case registry.DesiredActionStop:
		if !found {
			return DiffNone
		}
		actualReplicas := actualReplicaCount(actual)
		if desired.Replicas > 0 && actualReplicas < desired.Replicas {
			return DiffNeedScaleOut
		}
		if desired.Replicas > 0 && actualReplicas > desired.Replicas {
			return DiffNeedScaleIn
		}
		if isActualRunning(actual) {
			return DiffNeedStop
		}
		return DiffNone
	default:
		return DiffNone
	}
}

func actualReplicaCount(actual ecsmclient.ServiceInfo) int {
	return actual.InstanceActive
}

func isActualRunning(actual ecsmclient.ServiceInfo) bool {
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

func logDiff(serviceName string, desired *registry.DesiredState, actual ecsmclient.ServiceInfo, found bool, diff DiffAction) {
	if found {
		log.Printf("[INFO] reconciler diff: service=%s desired_action=%s desired_replicas=%s actual_status=%s actual_online=%d actual_active=%d diff=%s",
			serviceName,
			desired.Action,
			formatDesiredReplicas(desired),
			actual.Status,
			actual.InstanceOnline,
			actual.InstanceActive,
			diff,
		)
		return
	}

	log.Printf("[INFO] reconciler diff: service=%s desired_action=%s desired_replicas=%s actual=<missing> diff=%s",
		serviceName,
		desired.Action,
		formatDesiredReplicas(desired),
		diff,
	)
}

func formatDesiredReplicas(desired *registry.DesiredState) string {
	if desired == nil || desired.Replicas <= 0 {
		return "<nil>"
	}
	return fmt.Sprintf("%d", desired.Replicas)
}
