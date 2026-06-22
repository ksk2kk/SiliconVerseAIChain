package storage

import (
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// LevelDB wraps goleveldb to implement the Database interface.
type LevelDB struct {
	db *leveldb.DB
}

// NewLevelDB opens or creates a LevelDB database at the given path.
func NewLevelDB(path string) (*LevelDB, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open leveldb at %s: %w", path, err)
	}
	return &LevelDB{db: db}, nil
}

// NewLevelDBWithOpts opens LevelDB with custom options.
func NewLevelDBWithOpts(path string, opts *opt.Options) (*LevelDB, error) {
	db, err := leveldb.OpenFile(path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open leveldb at %s: %w", path, err)
	}
	return &LevelDB{db: db}, nil
}

// Put stores a key-value pair.
func (l *LevelDB) Put(key []byte, value []byte) error {
	return l.db.Put(key, value, nil)
}

// Get retrieves a value by key. Returns nil without error if key not found.
func (l *LevelDB) Get(key []byte) ([]byte, error) {
	val, err := l.db.Get(key, nil)
	if err == leveldb.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Has checks if a key exists.
func (l *LevelDB) Has(key []byte) (bool, error) {
	return l.db.Has(key, nil)
}

// Delete removes a key.
func (l *LevelDB) Delete(key []byte) error {
	return l.db.Delete(key, nil)
}

// Close terminates the database connection.
func (l *LevelDB) Close() error {
	return l.db.Close()
}

// NewBatch creates a write batch.
func (l *LevelDB) NewBatch() Batch {
	return &LevelDBBatch{
		db:    l.db,
		batch: new(leveldb.Batch),
	}
}

// NewIterator creates a snapshot iterator with the given prefix.
func (l *LevelDB) NewIterator() Iterator {
	iter := l.db.NewIterator(&util.Range{}, nil)
	return &LevelDBIterator{iter: iter}
}

// NewIteratorWithPrefix creates an iterator over keys with the given prefix.
func (l *LevelDB) NewIteratorWithPrefix(prefix []byte) Iterator {
	iter := l.db.NewIterator(util.BytesPrefix(prefix), nil)
	return &LevelDBIterator{iter: iter}
}

// LevelDBBatch implements Batch using leveldb.Batch.
type LevelDBBatch struct {
	db    *leveldb.DB
	batch *leveldb.Batch
}

func (b *LevelDBBatch) Put(key, value []byte) {
	b.batch.Put(key, value)
}

func (b *LevelDBBatch) Delete(key []byte) {
	b.batch.Delete(key)
}

func (b *LevelDBBatch) Write() error {
	return b.db.Write(b.batch, nil)
}

func (b *LevelDBBatch) Reset() {
	b.batch.Reset()
}

func (b *LevelDBBatch) ValueSize() int {
	return len(b.batch.Dump())
}

// LevelDBIterator wraps the leveldb iterator.
type LevelDBIterator struct {
	iter iterator.Iterator
}

func (it *LevelDBIterator) Next() bool {
	return it.iter.Next()
}

func (it *LevelDBIterator) Key() []byte {
	return it.iter.Key()
}

func (it *LevelDBIterator) Value() []byte {
	return it.iter.Value()
}

func (it *LevelDBIterator) Error() error {
	return it.iter.Error()
}

func (it *LevelDBIterator) Release() {
	it.iter.Release()
}
