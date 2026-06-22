package storage

// Database is the persistent key-value storage interface.
type Database interface {
	Put(key []byte, value []byte) error
	Get(key []byte) ([]byte, error)
	Has(key []byte) (bool, error)
	Delete(key []byte) error
	Close() error
	NewBatch() Batch
	NewIterator() Iterator
}

// Batch allows atomic grouped writes.
type Batch interface {
	Put(key, value []byte)
	Delete(key []byte)
	Write() error
	Reset()
	ValueSize() int
}

// Iterator supports ordered key-value scanning.
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Error() error
	Release()
}
