package asynqwrap

import (
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
)

type spfyTaskDoc struct {
	Kind string `json:"kind"`

	ID    string `json:"id"`
	Queue string `json:"queue"`
	State string `json:"state"`
	Type  string `json:"type"`

	Payload []byte            `json:"payload,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Result  []byte            `json:"result,omitempty"`

	MaxRetry int `json:"maxRetry"`
	Retried  int `json:"retried"`

	LastErr      string    `json:"lastErr,omitempty"`
	LastFailedAt time.Time `json:"lastFailedAt"`

	CreatedAt     time.Time `json:"createdAt"`
	ProcessAt     time.Time `json:"processAt"`
	NextProcessAt time.Time `json:"nextProcessAt"`
	CompletedAt   time.Time `json:"completedAt"`

	CreatedAtUnixNano     int64 `json:"createdAtUnixNano"`
	ProcessAtUnixNano     int64 `json:"processAtUnixNano,omitempty"`
	NextProcessAtUnixNano int64 `json:"nextProcessAtUnixNano,omitempty"`
	CompletedAtUnixNano   int64 `json:"completedAtUnixNano,omitempty"`
	LastFailedAtUnixNano  int64 `json:"lastFailedAtUnixNano,omitempty"`

	TimeoutNanos     int64     `json:"timeoutNanos,omitempty"`
	Deadline         time.Time `json:"deadline"`
	DeadlineUnixNano int64     `json:"deadlineUnixNano,omitempty"`
	RetentionNanos   int64     `json:"retentionNanos,omitempty"`

	Group string `json:"group,omitempty"`

	UniqueKey             string    `json:"uniqueKey,omitempty"`
	UniqueExpires         time.Time `json:"uniqueExpires"`
	UniqueExpiresUnixNano int64     `json:"uniqueExpiresUnixNano,omitempty"`

	LeaseID            string    `json:"leaseId,omitempty"`
	LeaseUntil         time.Time `json:"leaseUntil"`
	LeaseUntilUnixNano int64     `json:"leaseUntilUnixNano,omitempty"`
	IsOrphaned         bool      `json:"isOrphaned,omitempty"`
}

type spfyQueueDoc struct {
	Kind           string    `json:"kind"`
	Queue          string    `json:"queue"`
	Paused         bool      `json:"paused"`
	ProcessedTotal int64     `json:"processedTotal"`
	FailedTotal    int64     `json:"failedTotal"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type spfyRecurringDoc struct {
	Kind      string    `json:"kind"`
	Key       string    `json:"key"`
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type spfyUniqueDoc struct {
	Kind      string    `json:"kind"`
	Key       string    `json:"key"`
	TaskID    string    `json:"taskId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type spfyStringSetDoc struct {
	Kind  string   `json:"kind"`
	Items []string `json:"items"`
}

func newSpfyTaskDoc(rec *taskRecord, stateName string) *spfyTaskDoc {
	info := rec.info
	doc := &spfyTaskDoc{
		Kind:           spfyKindTask,
		ID:             info.ID,
		Queue:          info.Queue,
		State:          stateName,
		Type:           info.Type,
		Payload:        append([]byte(nil), info.Payload...),
		Headers:        cloneHeaders(info.Headers),
		MaxRetry:       info.MaxRetry,
		Retried:        info.Retried,
		LastErr:        info.LastErr,
		LastFailedAt:   info.LastFailedAt,
		CreatedAt:      rec.createdAt,
		ProcessAt:      rec.processAt,
		NextProcessAt:  info.NextProcessAt,
		CompletedAt:    info.CompletedAt,
		TimeoutNanos:   int64(info.Timeout),
		Deadline:       info.Deadline,
		RetentionNanos: int64(rec.retention),
		Group:          info.Group,
		UniqueKey:      rec.uniqueKey,
		UniqueExpires:  rec.uniqueExpires,
		Result:         append([]byte(nil), info.Result...),
	}
	doc.refreshUnixFields()
	return doc
}

func (d *spfyTaskDoc) clone() *spfyTaskDoc {
	if d == nil {
		return nil
	}
	out := *d
	out.Payload = append([]byte(nil), d.Payload...)
	out.Headers = cloneHeaders(d.Headers)
	out.Result = append([]byte(nil), d.Result...)
	return &out
}

func (d *spfyTaskDoc) refreshUnixFields() {
	if d == nil {
		return
	}
	d.CreatedAtUnixNano = timeUnixNano(d.CreatedAt)
	d.ProcessAtUnixNano = timeUnixNano(d.ProcessAt)
	d.NextProcessAtUnixNano = timeUnixNano(d.NextProcessAt)
	d.CompletedAtUnixNano = timeUnixNano(d.CompletedAt)
	d.LastFailedAtUnixNano = timeUnixNano(d.LastFailedAt)
	d.DeadlineUnixNano = timeUnixNano(d.Deadline)
	d.UniqueExpiresUnixNano = timeUnixNano(d.UniqueExpires)
	d.LeaseUntilUnixNano = timeUnixNano(d.LeaseUntil)
}

func (d *spfyTaskDoc) taskInfo() *asynq.TaskInfo {
	if d == nil {
		return nil
	}
	return &asynq.TaskInfo{
		ID:            d.ID,
		Queue:         d.Queue,
		Type:          d.Type,
		Payload:       append([]byte(nil), d.Payload...),
		State:         taskStateFromString(d.State),
		MaxRetry:      d.MaxRetry,
		Retried:       d.Retried,
		LastErr:       d.LastErr,
		LastFailedAt:  d.LastFailedAt,
		Timeout:       time.Duration(d.TimeoutNanos),
		Deadline:      d.Deadline,
		Group:         d.Group,
		NextProcessAt: d.NextProcessAt,
		IsOrphaned:    d.IsOrphaned,
		Retention:     time.Duration(d.RetentionNanos),
		CompletedAt:   d.CompletedAt,
		Result:        append([]byte(nil), d.Result...),
		Headers:       cloneHeaders(d.Headers),
	}
}

func (d *spfyTaskDoc) taskRecord() *taskRecord {
	if d == nil {
		return nil
	}
	info := d.taskInfo()
	return &taskRecord{
		task:          asynq.NewTaskWithHeaders(d.Type, append([]byte(nil), d.Payload...), cloneHeaders(d.Headers)),
		info:          info,
		createdAt:     d.CreatedAt,
		processAt:     d.ProcessAt,
		retention:     time.Duration(d.RetentionNanos),
		uniqueKey:     d.UniqueKey,
		uniqueExpires: d.UniqueExpires,
		heapIndex:     -1,
	}
}

func decodeSpfyTaskDoc(data []byte) (*spfyTaskDoc, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc spfyTaskDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	doc.refreshUnixFields()
	return &doc, nil
}

func decodeSpfyQueueDoc(data []byte) (*spfyQueueDoc, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc spfyQueueDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func decodeSpfyRecurringDoc(data []byte) (*spfyRecurringDoc, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc spfyRecurringDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func decodeSpfyStringSetDoc(data []byte) (*spfyStringSetDoc, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc spfyStringSetDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	doc.Items = normalizeStringSet(doc.Items)
	return &doc, nil
}

func buildStringSetDoc(kind string, current *spfyStringSetDoc, items ...string) *spfyStringSetDoc {
	out := &spfyStringSetDoc{Kind: kind}
	if current != nil {
		out.Items = append(out.Items, current.Items...)
	}
	out.Items = append(out.Items, items...)
	out.Items = normalizeStringSet(out.Items)
	return out
}

func removeFromStringSetDoc(current *spfyStringSetDoc, items ...string) *spfyStringSetDoc {
	if current == nil {
		return &spfyStringSetDoc{Kind: spfyKindTaskSet}
	}
	remove := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		remove[item] = struct{}{}
	}
	out := &spfyStringSetDoc{Kind: spfyKindTaskSet}
	for _, item := range current.Items {
		if _, ok := remove[item]; ok || item == "" {
			continue
		}
		out.Items = append(out.Items, item)
	}
	out.Items = normalizeStringSet(out.Items)
	return out
}

func groupReadyDocs(envs []*spfyTaskEnvelope, cfg asynq.Config, now time.Time) bool {
	if cfg.GroupMaxSize > 0 && len(envs) >= cfg.GroupMaxSize {
		return true
	}
	var first, last time.Time
	for _, env := range envs {
		doc, _ := env.snapshot()
		if first.IsZero() || doc.CreatedAt.Before(first) {
			first = doc.CreatedAt
		}
		if last.IsZero() || doc.CreatedAt.After(last) {
			last = doc.CreatedAt
		}
	}
	grace := cfg.GroupGracePeriod
	if grace <= 0 {
		grace = time.Minute
	}
	if now.Sub(last) >= grace {
		return true
	}
	if cfg.GroupMaxDelay > 0 && now.Sub(first) >= cfg.GroupMaxDelay {
		return true
	}
	return false
}
