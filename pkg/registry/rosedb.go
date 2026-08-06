package registry

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/rosedblabs/rosedb/v2"
)

// RoseDBOptions 是打开 RoseDB 存储时需要的配置。
type RoseDBOptions struct {
	DirPath        string
	Sync           bool
	WatchQueueSize uint64
}

// RoseDBStore 是基于 RoseDB 的 Store 实现。
type RoseDBStore struct {
	db *rosedb.DB
}

// OpenRoseDB 打开一个 RoseDBStore。
// WatchQueueSize 大于 0 时会开启 RoseDB watch，用于监听 desired state 变更。
func OpenRoseDB(opts RoseDBOptions) (*RoseDBStore, error) {
	dbOpts := rosedb.DefaultOptions
	dbOpts.DirPath = opts.DirPath
	dbOpts.Sync = opts.Sync
	dbOpts.WatchQueueSize = opts.WatchQueueSize

	db, err := rosedb.Open(dbOpts)
	if err != nil {
		return nil, err
	}
	return &RoseDBStore{db: db}, nil
}

// NewRoseDBStore 使用已有的 RoseDB 实例创建 Store，便于测试或复用外部生命周期。
func NewRoseDBStore(db *rosedb.DB) *RoseDBStore {
	return &RoseDBStore{db: db}
}

// Close 关闭底层 RoseDB 实例。
func (s *RoseDBStore) Close() error {
	return s.db.Close()
}

// PutDesired 写入或覆盖指定服务的 desired state。
func (s *RoseDBStore) PutDesired(ctx context.Context, state DesiredState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateDesiredState(state); err != nil {
		return err
	}
	return s.putJSON(DesiredServiceKey(state.ServiceName), state)
}

// UpdateDesired 修改指定服务的 desired state。
// 当前语义是 upsert：服务记录不存在时会创建，存在时会覆盖。
func (s *RoseDBStore) UpdateDesired(ctx context.Context, state DesiredState) error {
	return s.PutDesired(ctx, state)
}

// GetDesired 读取指定服务的 desired state。
func (s *RoseDBStore) GetDesired(ctx context.Context, serviceName string) (*DesiredState, error) {
	if err := validateServiceName(serviceName); err != nil {
		return nil, err
	}
	var state DesiredState
	if err := s.getJSON(ctx, DesiredServiceKey(serviceName), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// QueryDesired 查询指定服务的 desired state。
func (s *RoseDBStore) QueryDesired(ctx context.Context, serviceName string) (*DesiredState, error) {
	return s.GetDesired(ctx, serviceName)
}

// DeleteDesired 删除指定服务的 desired state。
func (s *RoseDBStore) DeleteDesired(ctx context.Context, serviceName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateServiceName(serviceName); err != nil {
		return err
	}
	return mapKeyNotFoundErr(s.db.Delete([]byte(DesiredServiceKey(serviceName))))
}

// ListDesired 按 desired/services/ 前缀列出所有服务的 desired state。
func (s *RoseDBStore) ListDesired(ctx context.Context) ([]DesiredState, error) {
	var states []DesiredState
	err := s.ascendPrefix(ctx, DesiredServicePrefix, func(_ string, value []byte) error {
		var state DesiredState
		if err := json.Unmarshal(value, &state); err != nil {
			return err
		}
		states = append(states, state)
		return nil
	})
	return states, err
}

// WatchDesired 监听 desired state 的变更，并只对外发送服务名。
// 调用方收到事件后应该入队，由 Reconciler 重新读取最新 desired state 再执行收敛。
func (s *RoseDBStore) WatchDesired(ctx context.Context) (<-chan DesiredEvent, error) {
	watchCh, err := s.db.Watch()
	if err != nil {
		return nil, mapWatchError(err)
	}

	out := make(chan DesiredEvent, 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watchCh:
				if !ok {
					return
				}
				serviceName, ok := ServiceNameFromDesiredKey(string(event.Key))
				if !ok {
					continue
				}
				desiredEvent := DesiredEvent{ServiceName: serviceName, Action: toEventAction(event.Action)}
				select {
				case out <- desiredEvent:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}

// PutStatus 写入或覆盖指定服务的收敛状态。
func (s *RoseDBStore) PutStatus(ctx context.Context, status ServiceStatus) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateServiceName(status.ServiceName); err != nil {
		return err
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	return s.putJSON(StatusServiceKey(status.ServiceName), status)
}

// GetStatus 读取指定服务的收敛状态。
func (s *RoseDBStore) GetStatus(ctx context.Context, serviceName string) (*ServiceStatus, error) {
	if err := validateServiceName(serviceName); err != nil {
		return nil, err
	}
	var status ServiceStatus
	if err := s.getJSON(ctx, StatusServiceKey(serviceName), &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// DeleteStatus 删除指定服务的收敛状态。
func (s *RoseDBStore) DeleteStatus(ctx context.Context, serviceName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateServiceName(serviceName); err != nil {
		return err
	}
	return mapKeyNotFoundErr(s.db.Delete([]byte(StatusServiceKey(serviceName))))
}

// ListStatus 按 status/services/ 前缀列出所有服务的收敛状态。
func (s *RoseDBStore) ListStatus(ctx context.Context) ([]ServiceStatus, error) {
	var statuses []ServiceStatus
	err := s.ascendPrefix(ctx, StatusServicePrefix, func(_ string, value []byte) error {
		var status ServiceStatus
		if err := json.Unmarshal(value, &status); err != nil {
			return err
		}
		statuses = append(statuses, status)
		return nil
	})
	return statuses, err
}

func (s *RoseDBStore) putJSON(key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.db.Put([]byte(key), payload)
}

func (s *RoseDBStore) getJSON(ctx context.Context, key string, dst any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := s.db.Get([]byte(key))
	if err := mapKeyNotFoundErr(err); err != nil {
		return err
	}
	return json.Unmarshal(value, dst)
}

// mapKeyNotFoundErr 将 RoseDB 的 key 不存在错误统一映射为 ErrNotFound。
func mapKeyNotFoundErr(err error) error {
	if errors.Is(err, rosedb.ErrKeyNotFound) {
		return ErrNotFound
	}
	return err
}

func (s *RoseDBStore) ascendPrefix(ctx context.Context, prefix string, handle func(key string, value []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var iterErr error
	s.db.AscendGreaterOrEqual([]byte(prefix), func(key []byte, value []byte) (bool, error) {
		if err := ctx.Err(); err != nil {
			iterErr = err
			return false, err
		}
		keyString := string(key)
		if len(keyString) < len(prefix) || keyString[:len(prefix)] != prefix {
			return false, nil
		}
		if err := handle(keyString, value); err != nil {
			iterErr = err
			return false, err
		}
		return true, nil
	})
	return iterErr
}

// mapWatchError 将 RoseDB watch 错误映射为 registry 层错误。
func mapWatchError(err error) error {
	if errors.Is(err, rosedb.ErrWatchDisabled) {
		return ErrWatchDisabled
	}
	return err
}

func toEventAction(action rosedb.WatchActionType) EventAction {
	if action == rosedb.WatchActionDelete {
		return EventActionDelete
	}
	return EventActionPut
}

// ValidateDesiredState 校验 desired state 是否符合存储和 VSOA API 约定。
func ValidateDesiredState(state DesiredState) error {
	return validateDesiredState(state)
}

func validateDesiredState(state DesiredState) error {
	if err := validateServiceName(state.ServiceName); err != nil {
		return err
	}
	switch state.Action {
	case DesiredActionStart, DesiredActionStop:
	default:
		return ErrInvalidDesiredAction
	}
	if state.Replicas < 0 {
		return ErrInvalidReplicas
	}
	return nil
}

var _ Store = (*RoseDBStore)(nil)
