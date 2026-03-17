package boltdbstore

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"go.etcd.io/bbolt"
)

var (
	indexBucket  = []byte("index")
	metaBucket   = []byte("meta")
	eventsBucket = []byte("events")
	aggsBucket   = []byte("aggregates")

	metaNextShard   = []byte("next_shard")
	metaMaxPerShard = []byte("max_per_shard")
)

const defaultMaxPerShard = 10_000

// BoltDBStore is a sharded BoltDB-backed implementation of the Store and
// AggregateStore interfaces. Data is spread across multiple BoltDB files
// keyed by aggregate ID, with an index DB tracking shard assignments.
type BoltDBStore struct {
	basePath    string
	pkg         string
	maxPerShard int

	mu      sync.RWMutex
	indexDB *bbolt.DB
	shards  map[uint32]*bbolt.DB
}

// Option configures a BoltDBStore.
type Option func(*BoltDBStore)

// WithMaxPerShard sets the maximum number of aggregate IDs per shard file.
// Default is 10,000.
func WithMaxPerShard(n int) Option {
	return func(s *BoltDBStore) {
		if n > 0 {
			s.maxPerShard = n
		}
	}
}

// New creates a new BoltDBStore. basePath is the root directory and pkg is
// the subdirectory name (e.g. the aggregate package). The directory
// basePath/pkg/ is created if it does not exist.
func New(basePath string, pkg string, opts ...Option) (*BoltDBStore, error) {
	s := &BoltDBStore{
		basePath:    basePath,
		pkg:         pkg,
		maxPerShard: defaultMaxPerShard,
		shards:      make(map[uint32]*bbolt.DB),
	}
	for _, opt := range opts {
		opt(s)
	}

	dir := filepath.Join(basePath, pkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	db, err := bbolt.Open(filepath.Join(dir, "index.db"), 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open index db: %w", err)
	}
	s.indexDB = db

	if err := s.initIndex(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *BoltDBStore) initIndex() error {
	return s.indexDB.Update(func(tx *bbolt.Tx) error {
		idx, err := tx.CreateBucketIfNotExists(indexBucket)
		if err != nil {
			return err
		}
		meta, err := tx.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return err
		}
		// Initialize defaults only if not already set.
		if meta.Get(metaNextShard) == nil {
			var buf [4]byte
			binary.BigEndian.PutUint32(buf[:], 1)
			if err := meta.Put(metaNextShard, buf[:]); err != nil {
				return err
			}
		}
		if meta.Get(metaMaxPerShard) == nil {
			var buf [4]byte
			binary.BigEndian.PutUint32(buf[:], uint32(s.maxPerShard))
			if err := meta.Put(metaMaxPerShard, buf[:]); err != nil {
				return err
			}
		}
		_ = idx // ensure bucket exists
		return nil
	})
}

// Save stores records for the given aggregate ID.
func (s *BoltDBStore) Save(ctx context.Context, aggregateID string, records ...*recordv1.Record) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("save failed: context error: %w", err)
	}

	shardNum, err := s.assignShard(aggregateID)
	if err != nil {
		return fmt.Errorf("assign shard: %w", err)
	}

	db, err := s.openShard(shardNum)
	if err != nil {
		return fmt.Errorf("open shard: %w", err)
	}

	return db.Update(func(tx *bbolt.Tx) error {
		eb, err := tx.CreateBucketIfNotExists(eventsBucket)
		if err != nil {
			return err
		}
		aggBkt, err := eb.CreateBucketIfNotExists([]byte(aggregateID))
		if err != nil {
			return err
		}
		for _, rec := range records {
			var key [8]byte
			binary.BigEndian.PutUint64(key[:], uint64(rec.GetVersion()))
			if err := aggBkt.Put(key[:], rec.GetData()); err != nil {
				return err
			}
		}
		return nil
	})
}

// Load retrieves the event history for the given aggregate ID.
func (s *BoltDBStore) Load(ctx context.Context, aggregateID string) (*historyv1.History, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load failed: context error: %w", err)
	}

	shardNum, found, err := s.lookupShard(aggregateID)
	if err != nil {
		return nil, fmt.Errorf("lookup shard: %w", err)
	}
	if !found {
		return &historyv1.History{}, nil
	}

	db, err := s.openShard(shardNum)
	if err != nil {
		return nil, fmt.Errorf("open shard: %w", err)
	}

	h := &historyv1.History{}
	err = db.View(func(tx *bbolt.Tx) error {
		eb := tx.Bucket(eventsBucket)
		if eb == nil {
			return nil
		}
		aggBkt := eb.Bucket([]byte(aggregateID))
		if aggBkt == nil {
			return nil
		}
		c := aggBkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			version := int64(binary.BigEndian.Uint64(k))
			data := make([]byte, len(v))
			copy(data, v)
			h.Records = append(h.Records, &recordv1.Record{
				Version: version,
				Data:    data,
			})
		}
		return nil
	})

	return h, err
}

// LoadTail returns the last n events for the given aggregate, ordered by
// version ascending. Uses cursor.Last() and walks backwards, then reverses.
func (s *BoltDBStore) LoadTail(ctx context.Context, aggregateID string, n int) (*historyv1.History, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load tail failed: context error: %w", err)
	}

	shardNum, found, err := s.lookupShard(aggregateID)
	if err != nil {
		return nil, fmt.Errorf("lookup shard: %w", err)
	}
	if !found {
		return &historyv1.History{}, nil
	}

	db, err := s.openShard(shardNum)
	if err != nil {
		return nil, fmt.Errorf("open shard: %w", err)
	}

	h := &historyv1.History{}
	err = db.View(func(tx *bbolt.Tx) error {
		eb := tx.Bucket(eventsBucket)
		if eb == nil {
			return nil
		}
		aggBkt := eb.Bucket([]byte(aggregateID))
		if aggBkt == nil {
			return nil
		}
		// Walk backwards from the end, collecting up to n records.
		c := aggBkt.Cursor()
		collected := 0
		for k, v := c.Last(); k != nil && collected < n; k, v = c.Prev() {
			version := int64(binary.BigEndian.Uint64(k))
			data := make([]byte, len(v))
			copy(data, v)
			h.Records = append(h.Records, &recordv1.Record{
				Version: version,
				Data:    data,
			})
			collected++
		}
		// Reverse to ascending version order.
		for i, j := 0, len(h.Records)-1; i < j; i, j = i+1, j-1 {
			h.Records[i], h.Records[j] = h.Records[j], h.Records[i]
		}
		return nil
	})
	return h, err
}

// SaveAggregate persists the serialized aggregate state and its current version.
func (s *BoltDBStore) SaveAggregate(ctx context.Context, aggregateID string, data []byte, version int64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("save aggregate failed: context error: %w", err)
	}

	shardNum, found, err := s.lookupShard(aggregateID)
	if err != nil {
		return fmt.Errorf("lookup shard: %w", err)
	}
	if !found {
		return fmt.Errorf("save aggregate: no shard assigned for %s", aggregateID)
	}

	db, err := s.openShard(shardNum)
	if err != nil {
		return fmt.Errorf("open shard: %w", err)
	}

	if len(data) > math.MaxInt-8 {
		return fmt.Errorf("save aggregate: data too large (%d bytes)", len(data))
	}

	return db.Update(func(tx *bbolt.Tx) error {
		ab, err := tx.CreateBucketIfNotExists(aggsBucket)
		if err != nil {
			return err
		}
		// Value: 8-byte big-endian version + data
		val := make([]byte, 8+len(data))
		binary.BigEndian.PutUint64(val[:8], uint64(version))
		copy(val[8:], data)
		return ab.Put([]byte(aggregateID), val)
	})
}

// LoadAggregate retrieves the most recently saved aggregate state.
// Returns nil data with version 0 if no aggregate has been saved.
func (s *BoltDBStore) LoadAggregate(ctx context.Context, aggregateID string) ([]byte, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, fmt.Errorf("load aggregate failed: context error: %w", err)
	}

	shardNum, found, err := s.lookupShard(aggregateID)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup shard: %w", err)
	}
	if !found {
		return nil, 0, nil
	}

	db, err := s.openShard(shardNum)
	if err != nil {
		return nil, 0, fmt.Errorf("open shard: %w", err)
	}

	var data []byte
	var version int64
	err = db.View(func(tx *bbolt.Tx) error {
		ab := tx.Bucket(aggsBucket)
		if ab == nil {
			return nil
		}
		val := ab.Get([]byte(aggregateID))
		if val == nil {
			return nil
		}
		if len(val) < 8 {
			return fmt.Errorf("corrupt aggregate value for %s", aggregateID)
		}
		version = int64(binary.BigEndian.Uint64(val[:8]))
		data = make([]byte, len(val)-8)
		copy(data, val[8:])
		return nil
	})

	return data, version, err
}

// Close closes the index DB and all open shard DBs.
func (s *BoltDBStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	for num, db := range s.shards {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.shards, num)
	}
	if s.indexDB != nil {
		if err := s.indexDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		s.indexDB = nil
	}
	return firstErr
}

// assignShard looks up an existing shard for aggregateID, or assigns one if new.
func (s *BoltDBStore) assignShard(aggregateID string) (uint32, error) {
	var shardNum uint32
	err := s.indexDB.Update(func(tx *bbolt.Tx) error {
		idx := tx.Bucket(indexBucket)
		meta := tx.Bucket(metaBucket)

		// Check if already assigned.
		if v := idx.Get([]byte(aggregateID)); v != nil {
			shardNum = binary.BigEndian.Uint32(v)
			return nil
		}

		// Read current shard and count how many aggregates are in it.
		nextShard := binary.BigEndian.Uint32(meta.Get(metaNextShard))
		count := s.countInShard(idx, nextShard)

		maxPerShard := int(binary.BigEndian.Uint32(meta.Get(metaMaxPerShard)))
		if count >= maxPerShard {
			nextShard++
			var buf [4]byte
			binary.BigEndian.PutUint32(buf[:], nextShard)
			if err := meta.Put(metaNextShard, buf[:]); err != nil {
				return err
			}
		}

		// Assign.
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], nextShard)
		if err := idx.Put([]byte(aggregateID), buf[:]); err != nil {
			return err
		}
		shardNum = nextShard
		return nil
	})
	return shardNum, err
}

// countInShard counts how many index entries point to the given shard number.
func (s *BoltDBStore) countInShard(idx *bbolt.Bucket, shardNum uint32) int {
	count := 0
	c := idx.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		if binary.BigEndian.Uint32(v) == shardNum {
			count++
		}
	}
	return count
}

// lookupShard returns the shard number for an aggregate ID. found is false
// if the aggregate has never been assigned.
func (s *BoltDBStore) lookupShard(aggregateID string) (uint32, bool, error) {
	var shardNum uint32
	var found bool
	err := s.indexDB.View(func(tx *bbolt.Tx) error {
		idx := tx.Bucket(indexBucket)
		if v := idx.Get([]byte(aggregateID)); v != nil {
			shardNum = binary.BigEndian.Uint32(v)
			found = true
		}
		return nil
	})
	return shardNum, found, err
}

// openShard returns the bbolt.DB for the given shard number, opening it
// lazily if needed.
func (s *BoltDBStore) openShard(num uint32) (*bbolt.DB, error) {
	s.mu.RLock()
	if db, ok := s.shards[num]; ok {
		s.mu.RUnlock()
		return db, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if db, ok := s.shards[num]; ok {
		return db, nil
	}

	path := filepath.Join(s.basePath, s.pkg, fmt.Sprintf("shard-%04d.db", num))
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open shard %d: %w", num, err)
	}
	s.shards[num] = db
	return db, nil
}
