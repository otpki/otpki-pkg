package asynqwrap

// Extracted internal listoptions composition logic to replicate default behaviour with in-memory impl

import (
	"reflect"

	"github.com/hibiken/asynq"
)

type listOption struct {
	pageSize int
	pageNum  int
}

const (
	// Page size used by default in list operation.
	defaultPageSize = 30

	// Page number used by default in list operation.
	defaultPageNum = 1
)

var (
	pageSizeType = reflect.ValueOf(asynq.PageSize(1)).Type()
	pageNumType  = reflect.ValueOf(asynq.Page(1)).Type()
)

func composeListOptions(opts ...asynq.ListOption) listOption {
	res := listOption{
		pageSize: defaultPageSize,
		pageNum:  defaultPageNum,
	}
	for _, opt := range opts {
		f := reflect.ValueOf(opt)
		t := f.Type()
		switch t {
		case pageSizeType:
			res.pageSize = int(f.Int())
		case pageNumType:
			res.pageNum = int(f.Int())
		}
	}
	return res
}
