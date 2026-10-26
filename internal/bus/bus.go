package bus

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	maxPayload   = 64 << 10
	defaultLimit = 100
	maxLimit     = 1000
)

var (
	ErrInvalidTopic    = errors.New("invalid topic")
	ErrInvalidPattern  = errors.New("invalid topic pattern")
	ErrInvalidPayload  = errors.New("payload must be a JSON object or null")
	ErrPayloadTooLarge = errors.New("payload too large")
	ErrSlowSubscriber  = errors.New("slow subscriber dropped")

	errClosed = errors.New("bus closed")
)

const schema = `
CREATE TABLE IF NOT EXISTS events(
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	topic TEXT NOT NULL,
	source TEXT NOT NULL,
	time INTEGER NOT NULL,
	payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cursors(
	consumer TEXT PRIMARY KEY,
	seq INTEGER NOT NULL
);`

type Event struct {
	Seq     int64           `json:"seq"`
	Topic   string          `json:"topic"`
	Source  string          `json:"source"`
	Time    time.Time       `json:"time"`
	Payload json.RawMessage `json:"payload"`
}

type Bus struct {
	db *sql.DB
	wg sync.WaitGroup

	mu     sync.Mutex
	last   int64
	subs   map[*Subscription]struct{}
	closed bool
}

func Open(path string) (*Bus, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve bus db path: %w", err)
	}
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, fmt.Errorf("open bus db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate bus db: %w", err)
	}
	var last int64
	if err := db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&last); err != nil {
		db.Close()
		return nil, fmt.Errorf("read last seq: %w", err)
	}
	return &Bus{db: db, last: last, subs: map[*Subscription]struct{}{}}, nil
}

func (b *Bus) Close() error {
	b.mu.Lock()
	b.closed = true
	for s := range b.subs {
		s.endLocked(errClosed)
	}
	b.mu.Unlock()
	b.wg.Wait()
	return b.db.Close()
}

func (b *Bus) Publish(ctx context.Context, topic, source string, payload json.RawMessage) (Event, error) {
	if !ValidTopic(topic) {
		return Event{}, ErrInvalidTopic
	}
	payload, err := normalizePayload(payload)
	if err != nil {
		return Event{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return Event{}, errClosed
	}
	now := time.Now().UnixNano()
	res, err := b.db.ExecContext(ctx, `INSERT INTO events(topic, source, time, payload) VALUES(?, ?, ?, ?)`, topic, source, now, string(payload))
	if err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return Event{}, fmt.Errorf("event seq: %w", err)
	}
	e := Event{Seq: seq, Topic: topic, Source: source, Time: time.Unix(0, now).UTC(), Payload: payload}
	b.last = seq
	b.fanoutLocked(e)
	return e, nil
}

func (b *Bus) Read(ctx context.Context, pattern string, after int64, limit int) ([]Event, error) {
	if !ValidPattern(pattern) {
		return nil, ErrInvalidPattern
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	return b.readRange(ctx, pattern, after, math.MaxInt64, min(limit, maxLimit))
}

func (b *Bus) SetCursor(ctx context.Context, consumer string, seq int64) error {
	if _, err := b.db.ExecContext(ctx, `INSERT INTO cursors(consumer, seq) VALUES(?, ?) ON CONFLICT(consumer) DO UPDATE SET seq = excluded.seq`, consumer, seq); err != nil {
		return fmt.Errorf("set cursor: %w", err)
	}
	return nil
}

func (b *Bus) Cursor(ctx context.Context, consumer string) (int64, error) {
	var seq int64
	err := b.db.QueryRowContext(ctx, `SELECT seq FROM cursors WHERE consumer = ?`, consumer).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get cursor: %w", err)
	}
	return seq, nil
}

func (b *Bus) readRange(ctx context.Context, pattern string, after, upto int64, limit int) ([]Event, error) {
	rows, err := b.db.QueryContext(ctx, `SELECT seq, topic, source, time, payload FROM events WHERE seq > ? AND seq <= ? AND topic GLOB ? ORDER BY seq LIMIT ?`, after, upto, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var e Event
		var nanos int64
		var payload string
		if err := rows.Scan(&e.Seq, &e.Topic, &e.Source, &nanos, &payload); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Time = time.Unix(0, nanos).UTC()
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return events, nil
}

func normalizePayload(payload json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return json.RawMessage("null"), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	if buf.Len() > maxPayload {
		return nil, ErrPayloadTooLarge
	}
	return json.RawMessage(buf.Bytes()), nil
}
