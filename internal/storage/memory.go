package storage

import "fmt"

// MemoryDB is an in-memory implementation of Database for testing.
type MemoryDB struct {
	data map[string][]byte
}

// NewMemoryDB creates a new in-memory database.
func NewMemoryDB() *MemoryDB {
	return &MemoryDB{
		data: make(map[string][]byte),
	}
}

func (m *MemoryDB) Put(key []byte, value []byte) error {
	k := string(key)
	v := make([]byte, len(value))
	copy(v, value)
	m.data[k] = v
	return nil
}

func (m *MemoryDB) Get(key []byte) ([]byte, error) {
	k := string(key)
	v, ok := m.data[k]
	if !ok {
		return nil, nil
	}
	result := make([]byte, len(v))
	copy(result, v)
	return result, nil
}

func (m *MemoryDB) Has(key []byte) (bool, error) {
	_, ok := m.data[string(key)]
	return ok, nil
}

func (m *MemoryDB) Delete(key []byte) error {
	delete(m.data, string(key))
	return nil
}

func (m *MemoryDB) Close() error {
	return nil
}

func (m *MemoryDB) NewBatch() Batch {
	return &memoryBatch{db: m}
}

func (m *MemoryDB) NewIterator() Iterator {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return &memoryIterator{
		db:    m,
		keys:  keys,
		index: -1,
	}
}

// memoryBatch implements Batch for MemoryDB.
type memoryBatch struct {
	db     *MemoryDB
	writes []struct{ key, value string }
}

func (b *memoryBatch) Put(key, value []byte) {
	b.writes = append(b.writes, struct{ key, value string }{
		key: string(key), value: string(value),
	})
}

func (b *memoryBatch) Delete(key []byte) {
	b.writes = append(b.writes, struct{ key, value string }{
		key: string(key), value: "",
	})
}

func (b *memoryBatch) Write() error {
	for _, w := range b.writes {
		if w.value == "" {
			delete(b.db.data, w.key)
		} else {
			b.db.data[w.key] = []byte(w.value)
		}
	}
	return nil
}

func (b *memoryBatch) Reset() {
	b.writes = nil
}

func (b *memoryBatch) ValueSize() int {
	return len(b.writes)
}

// memoryIterator implements Iterator for MemoryDB.
type memoryIterator struct {
	db    *MemoryDB
	keys  []string
	index int
}

func (it *memoryIterator) Next() bool {
	it.index++
	return it.index < len(it.keys)
}

func (it *memoryIterator) Key() []byte {
	if it.index < 0 || it.index >= len(it.keys) {
		return nil
	}
	return []byte(it.keys[it.index])
}

func (it *memoryIterator) Value() []byte {
	if it.index < 0 || it.index >= len(it.keys) {
		return nil
	}
	v := it.db.data[it.keys[it.index]]
	result := make([]byte, len(v))
	copy(result, v)
	return result
}

func (it *memoryIterator) Error() error {
	return nil
}

func (it *memoryIterator) Release() {}

// Compile-time check
var _ Database = (*MemoryDB)(nil)

// Ensure errors implement error interface
var _ error = (*DBError)(nil)

// DBError wraps database errors.
type DBError struct {
	Op  string
	Key string
	Err error
}

func (e *DBError) Error() string {
	return fmt.Sprintf("storage: %s key=%q: %v", e.Op, e.Key, e.Err)
}

func (e *DBError) Unwrap() error {
	return e.Err
}
