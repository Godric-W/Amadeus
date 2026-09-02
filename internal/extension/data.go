package extension

import (
	"errors"
	"reflect"
	"strings"
	"sync"
)

type Data struct {
	levelID string
	mu      sync.RWMutex
	values  map[reflect.Type]any
}

func NewData(levelID string) (*Data, error) {
	levelID = strings.TrimSpace(levelID)
	if levelID == "" {
		return nil, errors.New("extension data level ID is empty")
	}
	return &Data{levelID: levelID, values: make(map[reflect.Type]any)}, nil
}

func (data *Data) LevelID() string {
	if data == nil {
		return ""
	}
	return data.levelID
}

func Get[T any](data *Data) (*T, bool) {
	if data == nil {
		return nil, false
	}
	data.mu.RLock()
	value, ok := data.values[typeOf[T]()]
	data.mu.RUnlock()
	if !ok {
		return nil, false
	}
	typed, ok := value.(*T)
	return typed, ok
}

func Set[T any](data *Data, value T) (*T, bool) {
	if data == nil {
		return nil, false
	}
	key := typeOf[T]()
	data.mu.Lock()
	previous, existed := data.values[key]
	stored := new(T)
	*stored = value
	data.values[key] = stored
	data.mu.Unlock()
	if !existed {
		return nil, false
	}
	typed, ok := previous.(*T)
	return typed, ok
}

func GetOrInit[T any](data *Data, init func() T) *T {
	if data == nil {
		return nil
	}
	key := typeOf[T]()
	data.mu.Lock()
	defer data.mu.Unlock()
	if value, ok := data.values[key]; ok {
		typed, _ := value.(*T)
		return typed
	}
	value := new(T)
	if init != nil {
		*value = init()
	}
	data.values[key] = value
	return value
}

func Remove[T any](data *Data) (*T, bool) {
	if data == nil {
		return nil, false
	}
	key := typeOf[T]()
	data.mu.Lock()
	value, ok := data.values[key]
	delete(data.values, key)
	data.mu.Unlock()
	if !ok {
		return nil, false
	}
	typed, ok := value.(*T)
	return typed, ok
}

func typeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }
