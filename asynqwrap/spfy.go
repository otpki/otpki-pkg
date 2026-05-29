package asynqwrap

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
	spfypubsub "github.com/otpki/otpki-pkg/spfy/pulse"
)

var _ Broker = (*spfyBroker)(nil)
var _ clientBackend = (*spfyClient)(nil)
var _ serverBackend = (*spfyServer)(nil)
var _ Inspector = (*spfyInspector)(nil)
var _ recurringStateStore = (*spfyRecurringStateStore)(nil)

const (
	spfyDefaultLeaseTTL     = 30 * time.Second
	spfyDefaultPollInterval = 500 * time.Millisecond
	spfyDefaultQueryLimit   = 100
	spfyContentTypeJSON     = "application/json"
	spfyMetadataTTL         = "ttlInSeconds"

	spfyKindTask      = "task"
	spfyKindQueue     = "queue"
	spfyKindQueueSet  = "queue-set"
	spfyKindTaskSet   = "task-set"
	spfyKindUnique    = "unique"
	spfyKindRecurring = "recurring-state"
)

// SpfyConfig configures ModeSpfy.
//
// The backend now uses spfy state-store and pub/sub interfaces. The state store
// is the durable source of truth for task state. Pub/sub is only used as a
// low-latency wake-up signal; workers also poll state so missed messages do not
// strand tasks.
type SpfyConfig struct {
	// StateStore stores queue metadata, tasks, unique locks, recurring task
	// state, and broker-maintained indexes. It must support ETags, TTL, and Batch.
	StateStore spfystate.Store[[]byte]
	// PubSub publishes and subscribes to queue wake-up notifications.
	PubSub spfypubsub.Backend

	// PubsubName is retained for source compatibility with the old Spfy-backed
	// configuration. spfy routes by logical subject, so this field is ignored.
	PubsubName string
	// StateMetadata is copied onto state writes after conversion to spfy metadata.
	StateMetadata map[string]string
	// PubSubMetadata is copied to spfy pub/sub message headers.
	PubSubMetadata map[string]string

	// WorkerLeaseTTL controls active-task leases. Workers heartbeat at roughly
	// half this interval. Default: 30 seconds.
	WorkerLeaseTTL time.Duration
	// PollInterval controls scheduled/retry promotion and fallback polling.
	// Default: 500 milliseconds.
	PollInterval time.Duration
	// QueryPageSize controls internal query pagination. Default: 100.
	QueryPageSize int

	// Now is optional and primarily useful for deterministic tests.
	Now func() time.Time
}

type spfyTaskEnvelope struct {
	key  string
	etag *string
	doc  *spfyTaskDoc
	mu   sync.Mutex
}

func (e *spfyTaskEnvelope) currentETag() *string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return cloneETag(e.etag)
}

func (e *spfyTaskEnvelope) replace(doc *spfyTaskDoc, etag *string) {
	e.mu.Lock()
	e.doc = doc
	e.etag = cloneETag(etag)
	e.mu.Unlock()
}

func (e *spfyTaskEnvelope) snapshot() (*spfyTaskDoc, *string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.doc.clone(), cloneETag(e.etag)
}

func cloneETag(etag *string) *string {
	if etag == nil {
		return nil
	}
	return new(*etag)
}

func etagString(etag spfystate.ETag) *string {
	if etag == "" {
		return nil
	}
	s := string(etag)
	return &s
}

func stateETag(etag *string) spfystate.ETag {
	if etag == nil {
		return ""
	}
	return spfystate.ETag(*etag)
}

func timeUnixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func taskStateFromString(s string) asynq.TaskState {
	switch s {
	case asynq.TaskStateActive.String():
		return asynq.TaskStateActive
	case asynq.TaskStatePending.String():
		return asynq.TaskStatePending
	case asynq.TaskStateScheduled.String():
		return asynq.TaskStateScheduled
	case asynq.TaskStateRetry.String():
		return asynq.TaskStateRetry
	case asynq.TaskStateArchived.String():
		return asynq.TaskStateArchived
	case asynq.TaskStateCompleted.String():
		return asynq.TaskStateCompleted
	case asynq.TaskStateAggregating.String():
		return asynq.TaskStateAggregating
	default:
		return 0
	}
}

type spfyStateOperation struct {
	Type     spfystate.OperationType
	Key      string
	Value    any
	Expected *string
	Options  spfystate.WriteOptions
}

func spfySetRequest(
	key string,
	value any,
	etag *string,
	firstWrite bool,
	metadata spfystate.Metadata,
) spfyStateOperation {
	opType := spfystate.OpPut
	if firstWrite {
		opType = spfystate.OpCreate
	} else if etag != nil {
		opType = spfystate.OpCompareAndSet
	}
	return spfyStateOperation{
		Type:     opType,
		Key:      key,
		Value:    value,
		Expected: cloneETag(etag),
		Options: spfystate.WriteOptions{
			TTL:      ttlFromStateMetadata(metadata),
			Metadata: metadata,
		},
	}
}

func spfyDeleteRequest(key string, etag *string, metadata spfystate.Metadata) spfyStateOperation {
	return spfyStateOperation{
		Type:     spfystate.OpDelete,
		Key:      key,
		Expected: cloneETag(etag),
		Options:  spfystate.WriteOptions{Metadata: metadata},
	}
}

func encodeStateValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return slices.Clone(v), nil
	default:
		return json.Marshal(v)
	}
}

func ttlFromStateMetadata(metadata spfystate.Metadata) time.Duration {
	if len(metadata) == 0 {
		return 0
	}
	values := metadata[spfyMetadataTTL]
	if len(values) == 0 {
		return 0
	}
	seconds, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func isETagConflict(err error) bool {
	return errors.Is(err, spfystate.ErrETagMismatch) ||
		errors.Is(err, spfystate.ErrConflict) ||
		errors.Is(err, spfystate.ErrAlreadyExists)
}

func normalizeStringSet(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	slices.Sort(out)
	return out
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
