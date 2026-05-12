package relocator

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

const (
	bucketProcessed = "processed"
	bucketStats     = "stats"
	bucketEvents    = "events"
	storeFilePerm   = 0o600
	eventKeyLen     = 8
)

// ProcessedEntry records the outcome of a single object pass.
type ProcessedEntry struct {
	ETag        string    `json:"etag"`
	Size        int64     `json:"size"`
	Status      string    `json:"status"`
	Files       int       `json:"files"`
	ProcessedAt time.Time `json:"processedAt"`
}

// BucketStats aggregates counters per source bucket.
type BucketStats struct {
	Name            string    `json:"name"`
	Downloaded      uint64    `json:"downloaded"`
	Extracted       uint64    `json:"extracted"`
	Posted          uint64    `json:"posted"`
	Deleted         uint64    `json:"deleted"`
	Skipped         uint64    `json:"skipped"`
	DownloadFailed  uint64    `json:"downloadFailed"`
	ExtractFailed   uint64    `json:"extractFailed"`
	PasswordFailed  uint64    `json:"passwordFailed"`
	PostFailed      uint64    `json:"postFailed"`
	BytesDownloaded uint64    `json:"bytesDownloaded"`
	LastActivity    time.Time `json:"lastActivity"`
	LastError       string    `json:"lastError"`
}

// Event is a single user-visible log entry surfaced on the status page.
type Event struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Bucket  string    `json:"bucket"`
	Object  string    `json:"object"`
	Message string    `json:"message"`
}

// Store is the bolt-backed persistence layer.
type Store struct {
	db     *bbolt.DB
	mu     sync.Mutex
	logCap int
}

// OpenStore opens (or creates) the bolt database at path.
func OpenStore(path string, eventLogSize int) (*Store, error) {
	boltDB, openErr := bbolt.Open(path, storeFilePerm, &bbolt.Options{Timeout: time.Second})
	if openErr != nil {
		return nil, fmt.Errorf("open bolt: %w", openErr)
	}

	initErr := boltDB.Update(func(tx *bbolt.Tx) error {
		for _, name := range []string{bucketProcessed, bucketStats, bucketEvents} {
			_, bucketErr := tx.CreateBucketIfNotExists([]byte(name))
			if bucketErr != nil {
				return fmt.Errorf("create bucket %s: %w", name, bucketErr)
			}
		}

		return nil
	})
	if initErr != nil {
		_ = boltDB.Close()

		return nil, fmt.Errorf("init bolt buckets: %w", initErr)
	}

	return &Store{db: boltDB, logCap: eventLogSize}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	closeErr := s.db.Close()
	if closeErr != nil {
		return fmt.Errorf("close bolt: %w", closeErr)
	}

	return nil
}

func processedKey(bucket, object string) []byte {
	return []byte(bucket + "\x00" + object)
}

// IsProcessed returns true if an object with the same etag was already handled.
func (s *Store) IsProcessed(bucket, object, etag string) (bool, error) {
	var found bool

	viewErr := s.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketProcessed))
		if bkt == nil {
			return nil
		}

		raw := bkt.Get(processedKey(bucket, object))
		if raw == nil {
			return nil
		}

		var entry ProcessedEntry

		unmarshalErr := json.Unmarshal(raw, &entry)
		if unmarshalErr != nil {
			return fmt.Errorf("decode processed entry: %w", unmarshalErr)
		}

		if entry.ETag == etag && entry.Status == "ok" {
			found = true
		}

		return nil
	})
	if viewErr != nil {
		return false, fmt.Errorf("bolt view: %w", viewErr)
	}

	return found, nil
}

// MarkProcessed stores the result of handling one object.
func (s *Store) MarkProcessed(bucket, object string, entry ProcessedEntry) error {
	payload, marshalErr := json.Marshal(entry)
	if marshalErr != nil {
		return fmt.Errorf("encode processed entry: %w", marshalErr)
	}

	updateErr := s.db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketProcessed))

		putErr := bkt.Put(processedKey(bucket, object), payload)
		if putErr != nil {
			return fmt.Errorf("put processed: %w", putErr)
		}

		return nil
	})
	if updateErr != nil {
		return fmt.Errorf("bolt update: %w", updateErr)
	}

	return nil
}

// UpdateStats applies fn under a write lock and persists the resulting stats.
func (s *Store) UpdateStats(bucket string, mutate func(*BucketStats)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	updateErr := s.db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketStats))

		var stats BucketStats

		raw := bkt.Get([]byte(bucket))
		if raw != nil {
			unmarshalErr := json.Unmarshal(raw, &stats)
			if unmarshalErr != nil {
				return fmt.Errorf("decode stats: %w", unmarshalErr)
			}
		}

		stats.Name = bucket
		mutate(&stats)

		stats.LastActivity = time.Now().UTC()

		payload, marshalErr := json.Marshal(stats)
		if marshalErr != nil {
			return fmt.Errorf("encode stats: %w", marshalErr)
		}

		putErr := bkt.Put([]byte(bucket), payload)
		if putErr != nil {
			return fmt.Errorf("put stats: %w", putErr)
		}

		return nil
	})
	if updateErr != nil {
		return fmt.Errorf("bolt update: %w", updateErr)
	}

	return nil
}

// AllStats returns a snapshot of every bucket's counters.
func (s *Store) AllStats() ([]BucketStats, error) {
	var out []BucketStats

	viewErr := s.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketStats))
		if bkt == nil {
			return nil
		}

		return bkt.ForEach(func(_, raw []byte) error {
			var stats BucketStats

			unmarshalErr := json.Unmarshal(raw, &stats)
			if unmarshalErr != nil {
				return fmt.Errorf("decode stats: %w", unmarshalErr)
			}

			out = append(out, stats)

			return nil
		})
	})
	if viewErr != nil {
		return nil, fmt.Errorf("bolt view: %w", viewErr)
	}

	return out, nil
}

// AppendEvent pushes a new event onto the ring buffer.
func (s *Store) AppendEvent(evt Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	updateErr := s.db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketEvents))

		seq, seqErr := bkt.NextSequence()
		if seqErr != nil {
			return fmt.Errorf("next seq: %w", seqErr)
		}

		evt.Time = time.Now().UTC()

		payload, marshalErr := json.Marshal(evt)
		if marshalErr != nil {
			return fmt.Errorf("encode event: %w", marshalErr)
		}

		key := make([]byte, eventKeyLen)
		binary.BigEndian.PutUint64(key, seq)

		putErr := bkt.Put(key, payload)
		if putErr != nil {
			return fmt.Errorf("put event: %w", putErr)
		}

		return s.trimEventsLocked(bkt)
	})
	if updateErr != nil {
		return fmt.Errorf("bolt update: %w", updateErr)
	}

	return nil
}

// RecentEvents returns up to limit most recent events, newest first.
func (s *Store) RecentEvents(limit int) ([]Event, error) {
	const defaultLimit = 100

	if limit <= 0 {
		limit = defaultLimit
	}

	out := make([]Event, 0, limit)

	viewErr := s.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketEvents))
		if bkt == nil {
			return nil
		}

		c := bkt.Cursor()
		for k, v := c.Last(); k != nil && len(out) < limit; k, v = c.Prev() {
			var evt Event

			unmarshalErr := json.Unmarshal(v, &evt)
			if unmarshalErr != nil {
				return fmt.Errorf("decode event: %w", unmarshalErr)
			}

			out = append(out, evt)
		}

		return nil
	})
	if viewErr != nil {
		return nil, fmt.Errorf("bolt view: %w", viewErr)
	}

	return out, nil
}

func (s *Store) trimEventsLocked(bkt *bbolt.Bucket) error {
	if s.logCap <= 0 {
		return nil
	}

	excess := bkt.Stats().KeyN - s.logCap
	if excess <= 0 {
		return nil
	}

	c := bkt.Cursor()
	for k, _ := c.First(); k != nil && excess > 0; k, _ = c.Next() {
		deleteErr := bkt.Delete(k)
		if deleteErr != nil {
			return fmt.Errorf("trim events: %w", deleteErr)
		}

		excess--
	}

	return nil
}
