package asynqwrap

import (
	"reflect"
	"unsafe"

	"github.com/hibiken/asynq"
)

func ensureTaskHeaders(task *asynq.Task) map[string]string {
	if headers := task.Headers(); headers != nil {
		return headers
	}

	headers := make(map[string]string)
	setTaskHeaders(task, headers)
	return headers
}

func setTaskHeaders(task *asynq.Task, headers map[string]string) {
	if task == nil {
		return
	}

	value := reflect.ValueOf(task)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return
	}

	field := value.Elem().FieldByName("headers")
	if !field.IsValid() || !field.CanAddr() {
		return
	}

	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(headers))
}

func taskOptions(task *asynq.Task) []asynq.Option {
	if task == nil {
		return nil
	}

	value := reflect.ValueOf(task)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return nil
	}

	field := value.Elem().FieldByName("opts")
	if !field.IsValid() || !field.CanAddr() {
		return nil
	}

	optsValue := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	if optsValue.IsNil() {
		return nil
	}

	opts := optsValue.Interface().([]asynq.Option)
	out := make([]asynq.Option, len(opts))
	copy(out, opts)
	return out
}
