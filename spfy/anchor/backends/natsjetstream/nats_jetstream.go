package natsjetstream

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
	"github.com/samber/oops"
)

type backend struct {
	nc          *nats.Conn
	js          jetstream.JetStream
	stream      jetstream.Stream
	name        string
	prefix      string
	opTimeout   time.Duration
	watchBuffer int
}

type jsStoredRecord struct {
	Event   anchor.EventType     `json:"event,omitempty"`
	Deleted bool                 `json:"deleted,omitempty"`
	Entry   internal.StoredEntry `json:"entry,omitempty"`
}

type jsPubAckResponse struct {
	Stream   string              `json:"stream"`
	Sequence uint64              `json:"seq"`
	Error    *jetstream.APIError `json:"error,omitempty"`
}

const (
	natsBatchIDHeader       = "Nats-Batch-Id"
	natsBatchSequenceHeader = "Nats-Batch-Sequence"
	natsBatchCommitHeader   = "Nats-Batch-Commit"
	natsMarkerReasonHeader  = "Nats-Marker-Reason"
)

func newBackend(
	ctx context.Context,
	nc *nats.Conn,
	cfg Config,
) (*backend, error) {
	defaults := DefaultConfig()
	if cfg.StreamName == "" {
		cfg.StreamName = defaults.StreamName
	}
	if cfg.Storage == "" {
		cfg.Storage = defaults.Storage
	}
	if cfg.OperationTimeout <= 0 {
		cfg.OperationTimeout = defaults.OperationTimeout
	}
	if cfg.SubjectDeleteMarkerTTL == 0 {
		cfg.SubjectDeleteMarkerTTL = defaults.SubjectDeleteMarkerTTL
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	prefix := cfg.SubjectPrefix
	if prefix == "" {
		prefix = "statestore." + sanitizeSubjectToken(cfg.StreamName) + ".data"
	}
	streamCtx, streamCancel := internal.NATSDeadlineContext(ctx, cfg.OperationTimeout)
	defer streamCancel()
	stream, err := js.CreateOrUpdateStream(streamCtx, jetstream.StreamConfig{
		Name:                   cfg.StreamName,
		Subjects:               []string{prefix + ".>"},
		Retention:              jetstream.LimitsPolicy,
		Storage:                cfg.Storage.jetStreamStorage(),
		MaxMsgsPerSubject:      1,
		Discard:                jetstream.DiscardOld,
		AllowAtomicPublish:     true,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: cfg.SubjectDeleteMarkerTTL,
		AllowDirect:            true,
	})
	if err != nil {
		return nil, err
	}
	return &backend{
		nc:          nc,
		js:          js,
		stream:      stream,
		name:        cfg.StreamName,
		prefix:      prefix,
		opTimeout:   cfg.OperationTimeout,
		watchBuffer: internal.NormalizeWatchBuffer(cfg.WatchBuffer),
	}, nil
}

func New[T any](
	ctx context.Context,
	nc *nats.Conn,
	cfg Config,
	codec anchor.Codec[T],
) (anchor.Store[T], error) {
	inner, err := newBackend(ctx, nc, cfg)
	if err != nil {
		return nil, err
	}
	return internal.NewStoreAdapter[T](inner, codec), nil
}

func (s *backend) Get(ctx context.Context, key string) (internal.StoredEntry, error) {
	entry, _, err := s.load(ctx, key)
	return entry, err
}

func (s *backend) Put(
	ctx context.Context,
	key string,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpPut, Key: key, Value: value, Options: opts}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *backend) Create(
	ctx context.Context,
	key string,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpCreate, Key: key, Value: value, Options: opts}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *backend) CompareAndSet(
	ctx context.Context,
	key string,
	expected anchor.ETag,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	res, err := s.Batch(
		ctx,
		[]internal.StoredOperation{
			{Type: anchor.OpCompareAndSet, Key: key, Expected: expected, Value: value, Options: opts},
		},
	)
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *backend) AppendMetadata(
	ctx context.Context,
	key string,
	metadata anchor.Metadata,
) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpMetadataAppend, Key: key, Metadata: metadata}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}
func (s *backend) Delete(ctx context.Context, key string) error {
	_, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpDelete, Key: key}})
	return err
}
func (s *backend) Consume(ctx context.Context, key string) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpConsume, Key: key}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *backend) Batch(ctx context.Context, ops []internal.StoredOperation) ([]internal.StoredEntry, error) {
	if err := internal.ValidateStoredOps(ops); err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	current := map[string]internal.StoredEntry{}
	lastSeq := map[string]uint64{}
	exists := map[string]bool{}
	for _, op := range ops {
		entry, seq, err := s.load(ctx, op.Key)
		if err == nil {
			current[op.Key], lastSeq[op.Key], exists[op.Key] = entry, seq, true
		} else if errors.Is(err, anchor.ErrNotFound) {
			exists[op.Key] = false
			lastSeq[op.Key] = seq
		} else {
			return nil, err
		}
	}
	for _, op := range ops {
		cur, ok := current[op.Key], exists[op.Key]
		switch op.Type {
		case anchor.OpCreate:
			if ok {
				return nil, anchor.ErrAlreadyExists
			}
		case anchor.OpCompareAndSet:
			if !ok {
				return nil, anchor.ErrNotFound
			}
			if cur.ETag != op.Expected {
				return nil, anchor.ErrETagMismatch
			}
		case anchor.OpMetadataAppend, anchor.OpDelete, anchor.OpConsume:
			if !ok {
				return nil, anchor.ErrNotFound
			}
		}
	}
	msgs := make([]*nats.Msg, 0, len(ops))
	results := make([]internal.StoredEntry, 0, len(ops))
	resultSlots := make([]int, 0, len(ops))
	updateResultETag := make([]bool, 0, len(ops))
	for _, op := range ops {
		cur := current[op.Key]
		var rec jsStoredRecord
		var ttl time.Duration
		switch op.Type {
		case anchor.OpPut, anchor.OpCreate, anchor.OpCompareAndSet:
			metadata := internal.CloneMetadata(op.Options.Metadata)
			if op.Type != anchor.OpCreate {
				metadata = internal.MergeMetadata(cur.Metadata, op.Options.Metadata)
			}
			entry := internal.StoredEntry{
				Key:       op.Key,
				Value:     append([]byte(nil), op.Value...),
				Metadata:  metadata,
				ExpiresAt: internal.ExpiresAt(now, op.Options.TTL),
			}
			rec = jsStoredRecord{Event: eventTypeForOperation(op.Type), Entry: entry}
			ttl = op.Options.TTL
			results = append(results, entry)
			resultSlots = append(resultSlots, len(msgs))
			updateResultETag = append(updateResultETag, true)
		case anchor.OpMetadataAppend:
			cur.Metadata = internal.MergeMetadata(cur.Metadata, op.Metadata)
			rec = jsStoredRecord{Event: anchor.EventMetadataAppend, Entry: cur}
			ttl = internal.TtlFromExpiresAt(cur.ExpiresAt)
			results = append(results, cur)
			resultSlots = append(resultSlots, len(msgs))
			updateResultETag = append(updateResultETag, true)
		case anchor.OpDelete:
			rec = jsStoredRecord{
				Event:   eventTypeForOperation(op.Type),
				Deleted: true,
				Entry:   internal.StoredEntry{Key: op.Key},
			}
		case anchor.OpConsume:
			// Consume returns the entry as it existed before deletion. The tombstone
			// gets a new stream sequence, but that sequence is not the consumed
			// entry's ETag. Keep cur.ETag unchanged in the result.
			rec = jsStoredRecord{Event: eventTypeForOperation(op.Type), Deleted: true, Entry: cur}
			results = append(results, cur)
			resultSlots = append(resultSlots, len(msgs))
			updateResultETag = append(updateResultETag, false)
		}
		inner, err := json.Marshal(rec)
		if err != nil {
			return nil, err
		}
		msg := &nats.Msg{Subject: s.subjectForKey(op.Key), Header: nats.Header{}, Data: inner}
		msg.Header.Set(jetstream.ExpectedStreamHeader, s.name)
		msg.Header.Set(jetstream.ExpectedLastSubjSeqHeader, strconv.FormatUint(lastSeq[op.Key], 10))
		if ttl > 0 {
			msg.Header.Set(jetstream.MsgTTLHeader, jetStreamTTLHeaderValue(ttl))
		}
		msgs = append(msgs, msg)
	}
	firstSeq, err := s.publishAtomic(ctx, msgs)
	if err != nil {
		return nil, mapJetStreamPublishErr(err)
	}
	for i, slot := range resultSlots {
		if !updateResultETag[i] {
			continue
		}
		seq := firstSeq + uint64(slot)
		results[i].ETag = anchor.ETag(strconv.FormatUint(seq, 10))
	}
	return results, nil
}

func (s *backend) Query(ctx context.Context, query anchor.Query) (internal.StoredQueryResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 1000
	}
	start := internal.DecodeOffsetCursor(query.Cursor)
	infoCtx, infoCancel := internal.NATSDeadlineContext(ctx, s.opTimeout)
	defer infoCancel()
	info, err := s.stream.Info(infoCtx, jetstream.WithSubjectFilter(s.prefix+".>"))
	if err != nil {
		return internal.StoredQueryResult{}, err
	}
	subjects := make([]string, 0, len(info.State.Subjects))
	for subj := range info.State.Subjects {
		subjects = append(subjects, subj)
	}
	sort.Strings(subjects)
	entries := make([]internal.StoredEntry, 0, limit)
	pos := 0
	for idx, subj := range subjects {
		if idx < start {
			continue
		}
		pos = idx + 1
		key, err := s.keyFromSubject(subj)
		if err != nil {
			continue
		}
		if query.Prefix != "" && !strings.HasPrefix(key, query.Prefix) {
			continue
		}
		entry, err := s.Get(ctx, key)
		if errors.Is(err, anchor.ErrNotFound) {
			continue
		}
		if err != nil {
			return internal.StoredQueryResult{}, err
		}
		if !internal.MetadataContains(entry.Metadata, query.Metadata) {
			continue
		}
		entries = append(entries, entry)
		if len(entries) >= limit {
			break
		}
	}
	var next string
	if pos < len(subjects) {
		next = internal.EncodeOffsetCursor(pos)
	}
	return internal.StoredQueryResult{Entries: entries, NextCursor: next}, nil
}

func (s *backend) Watch(ctx context.Context, opts anchor.WatchOptions) (<-chan internal.StoredEvent, error) {
	out := make(chan internal.StoredEvent, s.watchBuffer)
	if opts.IncludeCurrent {
		res, err := s.Query(ctx, anchor.Query{Prefix: opts.Prefix, Limit: 0})
		if err != nil {
			close(out)
			return nil, err
		}
		for _, e := range res.Entries {
			if internal.WatchMatches(opts, e.Key) {
				copy := internal.CloneStoredEntry(e)
				out <- internal.StoredEventFor(anchor.EventPut, e.Key, &copy, time.Now().UTC())
			}
		}
	}
	msgCh := make(chan *nats.Msg, s.watchBuffer)
	sub, err := s.nc.ChanSubscribe(s.prefix+".>", msgCh)
	if err != nil {
		close(out)
		return nil, err
	}
	flushCtx, flushCancel := internal.NATSDeadlineContext(ctx, s.opTimeout)
	_ = s.nc.FlushWithContext(flushCtx)
	flushCancel()
	go func() {
		defer close(out)
		defer sub.Unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				ev, ok := s.eventFromMsg(msg)
				if ok && internal.WatchMatches(opts, ev.Key) {
					select {
					case out <- ev:
					default:
					}
				}
			}
		}
	}()
	return out, nil
}

func (s *backend) load(ctx context.Context, key string) (internal.StoredEntry, uint64, error) {
	getCtx, getCancel := internal.NATSDeadlineContext(ctx, s.opTimeout)
	defer getCancel()
	inner, err := s.stream.GetLastMsgForSubject(getCtx, s.subjectForKey(key))
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return internal.StoredEntry{}, 0, anchor.ErrNotFound
	}
	if err != nil {
		return internal.StoredEntry{}, 0, err
	}
	if inner.Header.Get(natsMarkerReasonHeader) != "" || len(inner.Data) == 0 {
		return internal.StoredEntry{}, inner.Sequence, anchor.ErrNotFound
	}
	entry, deleted, err := decodeJSRecord(inner.Data, inner.Sequence)
	if err != nil {
		return internal.StoredEntry{}, inner.Sequence, err
	}
	if deleted || internal.StoredExpired(entry, time.Now().UTC()) {
		return internal.StoredEntry{}, inner.Sequence, anchor.ErrNotFound
	}
	return entry, inner.Sequence, nil
}

func (s *backend) publishAtomic(ctx context.Context, msgs []*nats.Msg) (uint64, error) {
	if len(msgs) == 0 {
		return 0, nil
	}
	requestCtx, requestCancel := internal.NATSDeadlineContext(ctx, s.opTimeout)
	defer requestCancel()
	batchID := string(anchor.NewETag())
	for i, msg := range msgs {
		msg.Header.Set(natsBatchIDHeader, batchID)
		msg.Header.Set(natsBatchSequenceHeader, strconv.Itoa(i+1))
		if i == len(msgs)-1 {
			msg.Header.Set(natsBatchCommitHeader, "1")
		}
		resp, err := s.nc.RequestMsgWithContext(requestCtx, msg)
		if err != nil {
			return 0, err
		}
		if i != len(msgs)-1 {
			continue
		}
		var ack jsPubAckResponse
		if err := json.Unmarshal(resp.Data, &ack); err != nil {
			return 0, err
		}
		if ack.Error != nil {
			return 0, ack.Error
		}
		if ack.Sequence == 0 {
			return 0, anchor.ErrETagMismatch
		}
		return ack.Sequence - uint64(len(msgs)) + 1, nil
	}
	return 0, oops.New("unreachable")
}

func (s *backend) eventFromMsg(msg *nats.Msg) (internal.StoredEvent, bool) {
	if msg.Header.Get(natsMarkerReasonHeader) != "" || len(msg.Data) == 0 {
		key, err := s.keyFromSubject(msg.Subject)
		if err != nil {
			return internal.StoredEvent{}, false
		}
		return internal.StoredEventFor(anchor.EventExpire, key, nil, time.Now().UTC()), true
	}
	entry, deleted, eventType, err := decodeJSRecordFull(msg.Data, 0)
	if err != nil {
		return internal.StoredEvent{}, false
	}
	if eventType == "" {
		if deleted {
			eventType = anchor.EventDelete
		} else {
			eventType = anchor.EventPut
		}
	}
	if deleted && eventType != anchor.EventConsume {
		return internal.StoredEventFor(eventType, entry.Key, nil, time.Now().UTC()), true
	}
	return internal.StoredEventFor(eventType, entry.Key, &entry, time.Now().UTC()), true
}

func decodeJSRecord(data []byte, seq uint64) (internal.StoredEntry, bool, error) {
	entry, deleted, _, err := decodeJSRecordFull(data, seq)
	return entry, deleted, err
}

func decodeJSRecordFull(data []byte, seq uint64) (internal.StoredEntry, bool, anchor.EventType, error) {
	var rec jsStoredRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return internal.StoredEntry{}, false, "", err
	}
	entry := rec.Entry
	if seq != 0 {
		entry.ETag = anchor.ETag(strconv.FormatUint(seq, 10))
	}
	return entry, rec.Deleted, rec.Event, nil
}

func jetStreamTTLHeaderValue(ttl time.Duration) string {
	// NATS JetStream per-message TTL rejects zero and sub-second durations.
	// Keep the logical ExpiresAt exactly as requested, but clamp the physical
	// storage cleanup TTL to the server-supported minimum.
	if ttl > 0 && ttl < time.Second {
		ttl = time.Second
	}
	return ttl.String()
}

func eventTypeForOperation(op anchor.OperationType) anchor.EventType {
	switch op {
	case anchor.OpPut:
		return anchor.EventPut
	case anchor.OpCreate:
		return anchor.EventCreate
	case anchor.OpCompareAndSet:
		return anchor.EventCompareAndSet
	case anchor.OpMetadataAppend:
		return anchor.EventMetadataAppend
	case anchor.OpDelete:
		return anchor.EventDelete
	case anchor.OpConsume:
		return anchor.EventConsume
	default:
		return anchor.EventPut
	}
}

func (s *backend) subjectForKey(key string) string {
	return s.prefix + "." + encodeSubjectKey(key)
}
func (s *backend) keyFromSubject(subject string) (string, error) {
	part := strings.TrimPrefix(subject, s.prefix+".")
	return decodeSubjectKey(part)
}

func encodeSubjectKey(key string) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(key))
}
func decodeSubjectKey(key string) (string, error) {
	inner, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(key)
	return string(inner), err
}
func sanitizeSubjectToken(in string) string {
	out := strings.Map(func(r rune) rune {
		if r == '.' || r == '*' || r == '>' || r == ' ' || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, in)
	if out == "" {
		return "default"
	}
	return out
}

func (s NATSJetStreamStorage) jetStreamStorage() jetstream.StorageType {
	switch s {
	case NATSJetStreamMemoryStorage:
		return jetstream.MemoryStorage
	default:
		return jetstream.FileStorage
	}
}

func mapJetStreamPublishErr(err error) error {
	if errors.Is(err, jetstream.ErrBadRequest) {
		return anchor.ErrETagMismatch
	}
	return err
}
