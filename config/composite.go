package config

import (
	"context"
	"fmt"
	"sync"

	"github.com/samber/oops"
)

type CompositeManager struct {
	managers sync.Map // map[string]any, name -> Manager[T]
	metadata sync.Map // map[string]*ManagerMetadata
}

type ManagerMetadata struct {
	Name string
	Type string // Type name for debugging
}

// NewCompositeManager creates a new composite configuration manager.
func NewCompositeManager() *CompositeManager {
	return &CompositeManager{}
}

// Register registers a configuration manager with a unique name.
func Register[T any](cm *CompositeManager, name string, manager *Manager[T]) error {
	// check if already exists
	if _, exists := cm.managers.LoadOrStore(name, manager); exists {
		return oops.In(ServiceName).Errorf("configuration manager %q already registered", name)
	}

	cm.metadata.Store(name, &ManagerMetadata{
		Name: name,
		Type: fmt.Sprintf("%T", new(T)),
	})

	return nil
}

// Get retrieves a configuration manager by name and type.
func Get[T any](cm *CompositeManager, name string) (*Manager[T], error) {
	mgr, exists := cm.managers.Load(name)
	if !exists {
		return nil, oops.In(ServiceName).Errorf("configuration manager %q not found", name)
	}

	typedMgr, ok := mgr.(*Manager[T])
	if !ok {
		return nil, oops.In(ServiceName).Errorf("configuration manager %q has wrong type", name)
	}

	return typedMgr, nil
}

// MustGet retrieves a configuration manager by name and type, panicking if not found.
func MustGet[T any](cm *CompositeManager, name string) *Manager[T] {
	mgr, err := Get[T](cm, name)
	if err != nil {
		panic(err)
	}
	return mgr
}

// LoadAll loads all registered configuration managers.
func (cm *CompositeManager) LoadAll(ctx context.Context) error {
	var errs []error

	cm.managers.Range(func(key, value any) bool {
		name := key.(string)
		if err := loadManager(ctx, value); err != nil {
			errs = append(errs, oops.In(ServiceName).Wrapf(err, "loading configuration manager %q", name))
		}
		return true
	})

	if len(errs) == 0 {
		return nil
	}

	if len(errs) == 1 {
		return errs[0]
	}

	return oops.In(ServiceName).Wrapf(oops.Join(errs...), "loading %d configuration managers", len(errs))
}

// ReloadAll reloads all registered configuration managers.
func (cm *CompositeManager) ReloadAll(ctx context.Context) error {
	var errs []error

	cm.managers.Range(func(key, value any) bool {
		name := key.(string)
		if err := reloadManager(ctx, value); err != nil {
			errs = append(errs, oops.In(ServiceName).Wrapf(err, "reloading configuration manager %q", name))
		}
		return true
	})

	if len(errs) == 0 {
		return nil
	}

	if len(errs) == 1 {
		return errs[0]
	}

	return oops.In(ServiceName).Wrapf(oops.Join(errs...), "reloading %d configuration managers", len(errs))
}

// Names returns all registered configuration manager names.
func (cm *CompositeManager) Names() []string {
	names := make([]string, 0)
	cm.managers.Range(func(key, value any) bool {
		names = append(names, key.(string))
		return true
	})
	return names
}

// Metadata returns metadata for a configuration manager.
func (cm *CompositeManager) Metadata(name string) (*ManagerMetadata, bool) {
	meta, ok := cm.metadata.Load(name)
	if !ok {
		return nil, false
	}
	return meta.(*ManagerMetadata), true
}

// AllMetadata returns metadata for all registered managers.
func (cm *CompositeManager) AllMetadata() map[string]*ManagerMetadata {
	result := make(map[string]*ManagerMetadata)
	cm.metadata.Range(func(key, value any) bool {
		result[key.(string)] = value.(*ManagerMetadata)
		return true
	})
	return result
}

// Count returns the number of registered managers.
func (cm *CompositeManager) Count() int {
	count := 0
	cm.managers.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

func loadManager(ctx context.Context, mgr any) error {
	switch m := mgr.(type) {
	case interface{ Load(context.Context) error }: // cannot use Loader[any] interface
		return m.Load(ctx)
	default:
		return oops.In(ServiceName).New("manager does not support Load")
	}
}

func reloadManager(ctx context.Context, mgr any) error {
	switch m := mgr.(type) {
	case interface{ Reload(context.Context) error }:
		return m.Reload(ctx)
	default:
		return oops.In(ServiceName).New("manager does not support Reload")
	}
}
