package asynqwrap

import (
	"container/heap"
	"net/http"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

const defaultMaxRetry = 25

type memoryBroker struct {
	mu   sync.Mutex
	cond *sync.Cond

	tasks      map[string]*taskRecord
	queues     map[string]*memoryQueue
	scheduled  taskHeap
	completed  map[string]*taskRecord
	archived   map[string]*taskRecord
	active     map[string]*taskRecord
	uniqueKeys map[string]time.Time

	serverSeq   uint64
	closed      bool
	maintenance chan struct{}

	groupAggregator  asynq.GroupAggregator
	groupGracePeriod time.Duration
	groupMaxDelay    time.Duration
	groupMaxSize     int

	recurringState recurringStateStore
	metrics        http.Handler
}

type memoryQueue struct {
	name           string
	paused         bool
	pending        []*taskRecord
	groups         map[string]*taskGroup
	processedTotal int64
	failedTotal    int64
}

type taskGroup struct {
	tasks   []*taskRecord
	firstAt time.Time
	lastAt  time.Time
}

type taskRecord struct {
	task          *asynq.Task
	info          *asynq.TaskInfo
	createdAt     time.Time
	processAt     time.Time
	retention     time.Duration
	uniqueKey     string
	uniqueExpires time.Time
	heapIndex     int
}

type taskHeap []*taskRecord

func (h taskHeap) Len() int { return len(h) }

func (h taskHeap) Less(i, j int) bool {
	return h[i].processAt.Before(h[j].processAt)
}

func (h taskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *taskHeap) Push(x any) {
	rec := x.(*taskRecord)
	rec.heapIndex = len(*h)
	*h = append(*h, rec)
}

func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	rec := old[n-1]
	old[n-1] = nil
	rec.heapIndex = -1
	*h = old[:n-1]
	return rec
}

var _ heap.Interface = (*taskHeap)(nil)
