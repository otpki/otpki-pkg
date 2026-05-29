package postgres

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
)

type backend struct {
	pool        *pgxpool.Pool
	table       string
	channel     string
	watchBuffer int
}

func newBackend(ctx context.Context, pool *pgxpool.Pool, cfg Config) (*backend, error) {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	if cfg.Table == "" {
		cfg.Table = DefaultConfig().Table
	}
	s := &backend{
		pool:        pool,
		table:       cfg.Table,
		channel:     notifyChannel(cfg.Table),
		watchBuffer: internal.NormalizeWatchBuffer(cfg.WatchBuffer),
	}
	if err := s.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func New[T any](
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg Config,
	codec anchor.Codec[T],
) (anchor.Store[T], error) {
	inner, err := newBackend(ctx, pool, cfg)
	if err != nil {
		return nil, err
	}
	return internal.NewStoreAdapter[T](inner, codec), nil
}

func (s *backend) ensureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  key text PRIMARY KEY,
  value bytea NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  etag text NOT NULL,
  expires_at timestamptz NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS %s ON %s (key);
CREATE INDEX IF NOT EXISTS %s ON %s USING gin (metadata);
CREATE INDEX IF NOT EXISTS %s ON %s (expires_at);
`, ident(s.table), ident(s.table+"_key_idx"), ident(s.table), ident(s.table+"_metadata_idx"), ident(s.table), ident(s.table+"_expires_idx"), ident(s.table)))
	return err
}

func (s *backend) Get(ctx context.Context, key string) (internal.StoredEntry, error) {
	entry, err := s.get(ctx, key)
	if err != nil {
		return internal.StoredEntry{}, err
	}
	if internal.StoredExpired(entry, time.Now().UTC()) {
		_ = s.Delete(ctx, key)
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	return entry, nil
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
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	current := map[string]internal.StoredEntry{}
	exists := map[string]bool{}
	for _, op := range ops {
		entry, ok, err := s.getForUpdate(ctx, tx, op.Key)
		if err != nil {
			return nil, err
		}
		if ok && internal.StoredExpired(entry, now) {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE key=$1`, ident(s.table)), op.Key); err != nil {
				return nil, err
			}
			ok = false
		}
		current[op.Key], exists[op.Key] = entry, ok
	}
	for _, op := range ops {
		entry := current[op.Key]
		ok := exists[op.Key]
		switch op.Type {
		case anchor.OpCreate:
			if ok {
				return nil, anchor.ErrAlreadyExists
			}
		case anchor.OpCompareAndSet:
			if !ok {
				return nil, anchor.ErrNotFound
			}
			if entry.ETag != op.Expected {
				return nil, anchor.ErrETagMismatch
			}
		case anchor.OpMetadataAppend, anchor.OpDelete, anchor.OpConsume:
			if !ok {
				return nil, anchor.ErrNotFound
			}
		}
	}
	results := make([]internal.StoredEntry, 0, len(ops))
	events := make([]internal.StoredEvent, 0, len(ops))
	for _, op := range ops {
		cur := current[op.Key]
		switch op.Type {
		case anchor.OpPut, anchor.OpCreate, anchor.OpCompareAndSet:
			metadata := internal.CloneMetadata(op.Options.Metadata)
			if op.Type != anchor.OpCreate {
				metadata = internal.MergeMetadata(cur.Metadata, op.Options.Metadata)
			}
			entry := internal.StoredEntry{
				Key:       op.Key,
				Value:     append([]byte(nil), op.Value...),
				ETag:      anchor.NewETag(),
				Metadata:  metadata,
				ExpiresAt: internal.ExpiresAt(now, op.Options.TTL),
			}
			if err := s.upsert(ctx, tx, entry); err != nil {
				return nil, err
			}
			results = append(results, internal.CloneStoredEntry(entry))
			typeMap := map[anchor.OperationType]anchor.EventType{
				anchor.OpPut:           anchor.EventPut,
				anchor.OpCreate:        anchor.EventCreate,
				anchor.OpCompareAndSet: anchor.EventCompareAndSet,
			}
			events = append(events, internal.StoredEventFor(typeMap[op.Type], op.Key, &entry, now))
		case anchor.OpMetadataAppend:
			cur.Metadata = internal.MergeMetadata(cur.Metadata, op.Metadata)
			cur.ETag = anchor.NewETag()
			if err := s.upsert(ctx, tx, cur); err != nil {
				return nil, err
			}
			copy := internal.CloneStoredEntry(cur)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventMetadataAppend, op.Key, &copy, now))
		case anchor.OpDelete:
			if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE key=$1`, ident(s.table)), op.Key); err != nil {
				return nil, err
			}
			events = append(events, internal.StoredEventFor(anchor.EventDelete, op.Key, nil, now))
		case anchor.OpConsume:
			if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE key=$1`, ident(s.table)), op.Key); err != nil {
				return nil, err
			}
			copy := internal.CloneStoredEntry(cur)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventConsume, op.Key, &copy, now))
		}
	}
	for _, ev := range events {
		if err := s.notifyTx(ctx, tx, ev); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40001" {
			return nil, anchor.ErrConflict
		}
		return nil, err
	}
	return results, nil
}

func (s *backend) Query(ctx context.Context, query anchor.Query) (internal.StoredQueryResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 1000
	}
	cursor := ""
	if query.Cursor != "" {
		if decoded, err := base64.RawURLEncoding.DecodeString(query.Cursor); err == nil {
			cursor = string(decoded)
		}
	}
	entries := make([]internal.StoredEntry, 0, limit)
	nextCursor := ""
	last := cursor
	for len(entries) < limit {
		rows, err := s.pool.Query(
			ctx,
			fmt.Sprintf(
				`SELECT key,value,metadata,etag,expires_at FROM %s WHERE key > $1 AND key LIKE $2 ESCAPE '\' AND (expires_at IS NULL OR expires_at > now()) ORDER BY key LIMIT $3`,
				ident(s.table),
			),
			last,
			escapeLikePrefix(query.Prefix)+"%",
			limit-len(entries)+1,
		)
		if err != nil {
			return internal.StoredQueryResult{}, err
		}
		var batch []internal.StoredEntry
		scannedCount := 0
		for rows.Next() {
			entry, err := scanStoredEntry(rows)
			if err != nil {
				rows.Close()
				return internal.StoredQueryResult{}, err
			}
			last = entry.Key
			scannedCount++
			if internal.MetadataContains(entry.Metadata, query.Metadata) {
				batch = append(batch, entry)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return internal.StoredQueryResult{}, err
		}
		rows.Close()
		for _, e := range batch {
			if len(entries) >= limit {
				nextCursor = base64.RawURLEncoding.EncodeToString([]byte(entries[len(entries)-1].Key))
				break
			}
			entries = append(entries, e)
		}
		if nextCursor != "" || scannedCount == 0 {
			break
		}
	}
	return internal.StoredQueryResult{Entries: entries, NextCursor: nextCursor}, nil
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
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		close(out)
		return nil, err
	}
	if _, err := conn.Exec(ctx, `LISTEN `+ident(s.channel)); err != nil {
		conn.Release()
		close(out)
		return nil, err
	}
	go func() {
		defer close(out)
		defer conn.Release()
		for {
			n, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				return
			}
			var ev internal.StoredEvent
			if json.Unmarshal([]byte(n.Payload), &ev) == nil && internal.WatchMatches(opts, ev.Key) {
				select {
				case out <- ev:
				default:
				}
			}
		}
	}()
	return out, nil
}

func (s *backend) get(ctx context.Context, key string) (internal.StoredEntry, error) {
	row := s.pool.QueryRow(
		ctx,
		fmt.Sprintf(`SELECT key,value,metadata,etag,expires_at FROM %s WHERE key=$1`, ident(s.table)),
		key,
	)
	entry, err := scanStoredEntry(row)
	if err == pgx.ErrNoRows {
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	return entry, err
}

func (s *backend) getForUpdate(ctx context.Context, tx pgx.Tx, key string) (internal.StoredEntry, bool, error) {
	row := tx.QueryRow(
		ctx,
		fmt.Sprintf(`SELECT key,value,metadata,etag,expires_at FROM %s WHERE key=$1 FOR UPDATE`, ident(s.table)),
		key,
	)
	entry, err := scanStoredEntry(row)
	if err == pgx.ErrNoRows {
		return internal.StoredEntry{}, false, nil
	}
	if err != nil {
		return internal.StoredEntry{}, false, err
	}
	return entry, true, nil
}

func (s *backend) upsert(ctx context.Context, tx pgx.Tx, entry internal.StoredEntry) error {
	metadata, err := json.Marshal(internal.CloneMetadata(entry.Metadata))
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s (key,value,metadata,etag,expires_at,updated_at) VALUES ($1,$2,$3,$4,$5,now()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, metadata=EXCLUDED.metadata, etag=EXCLUDED.etag, expires_at=EXCLUDED.expires_at, updated_at=now()`,
			ident(s.table),
		),
		entry.Key,
		entry.Value,
		metadata,
		string(entry.ETag),
		entry.ExpiresAt,
	)
	return err
}

func (s *backend) notifyTx(ctx context.Context, tx pgx.Tx, ev internal.StoredEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT pg_notify($1, $2)`, s.channel, string(payload))
	return err
}

func notifyChannel(table string) string {
	const maxIdentLen = 63
	suffix := "_changed"

	raw := table + suffix
	if len(raw) <= maxIdentLen {
		return raw
	}

	sum := sha1.Sum([]byte(raw))
	hash := hex.EncodeToString(sum[:])[:12]

	prefixLen := maxIdentLen - len(suffix) - 1 - len(hash)
	if prefixLen < 0 {
		prefixLen = 0
	}

	return table[:min(len(table), prefixLen)] + "_" + hash + suffix
}

type pgRow interface{ Scan(dest ...any) error }

func scanStoredEntry(row pgRow) (internal.StoredEntry, error) {
	var entry internal.StoredEntry
	var meta []byte
	var expires pgtype.Timestamptz
	if err := row.Scan(&entry.Key, &entry.Value, &meta, &entry.ETag, &expires); err != nil {
		return internal.StoredEntry{}, err
	}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &entry.Metadata)
	}
	if expires.Valid {
		t := expires.Time.UTC()
		entry.ExpiresAt = &t
	}
	return entry, nil
}

func ident(v string) string { return pgx.Identifier(strings.Split(v, ".")).Sanitize() }

func escapeLikePrefix(prefix string) string {
	return strings.NewReplacer(`\\`, `\\\\`, `%`, `\\%`, `_`, `\\_`).Replace(prefix)
}
