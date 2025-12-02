// Package transactionalmap provides a generic map with transaction support,
// including snapshot isolation, pre-commit and post-commit hooks.
package transactionalmap

import (
	"errors"
	"sync"
)

// ErrKeyNotFound is returned when a key is not found in the map.
var ErrKeyNotFound = errors.New("key not found")

// ErrTransactionClosed is returned when trying to use a closed transaction.
var ErrTransactionClosed = errors.New("transaction is closed")

// HookFunc is the type for pre-commit and post-commit hook functions.
// It receives the key and value being committed.
type HookFunc[K comparable, V any] func(key K, value V) error

// DeleteHookFunc is the type for hooks on delete operations.
type DeleteHookFunc[K comparable] func(key K) error

// Map is a generic map that supports transactions with snapshot isolation.
type Map[K comparable, V any] struct {
	mu              sync.RWMutex
	data            map[K]V
	preCommitHooks  []HookFunc[K, V]
	postCommitHooks []HookFunc[K, V]
	preDeleteHooks  []DeleteHookFunc[K]
	postDeleteHooks []DeleteHookFunc[K]
}

// New creates a new transactional map.
func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		data:            make(map[K]V),
		preCommitHooks:  make([]HookFunc[K, V], 0),
		postCommitHooks: make([]HookFunc[K, V], 0),
		preDeleteHooks:  make([]DeleteHookFunc[K], 0),
		postDeleteHooks: make([]DeleteHookFunc[K], 0),
	}
}

// AddPreCommitHook adds a hook that will be called before each key-value pair is committed.
// If the hook returns an error, the commit will be aborted.
func (m *Map[K, V]) AddPreCommitHook(hook HookFunc[K, V]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preCommitHooks = append(m.preCommitHooks, hook)
}

// AddPostCommitHook adds a hook that will be called after each key-value pair is committed.
func (m *Map[K, V]) AddPostCommitHook(hook HookFunc[K, V]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postCommitHooks = append(m.postCommitHooks, hook)
}

// AddPreDeleteHook adds a hook that will be called before each key is deleted.
// If the hook returns an error, the delete will be aborted.
func (m *Map[K, V]) AddPreDeleteHook(hook DeleteHookFunc[K]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preDeleteHooks = append(m.preDeleteHooks, hook)
}

// AddPostDeleteHook adds a hook that will be called after each key is deleted.
func (m *Map[K, V]) AddPostDeleteHook(hook DeleteHookFunc[K]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postDeleteHooks = append(m.postDeleteHooks, hook)
}

// Get retrieves a value by key directly from the map (not in a transaction).
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.data[key]
	return value, ok
}

// Set sets a value directly in the map (not in a transaction).
// This will trigger pre-commit and post-commit hooks.
func (m *Map[K, V]) Set(key K, value V) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Run pre-commit hooks
	for _, hook := range m.preCommitHooks {
		if err := hook(key, value); err != nil {
			return err
		}
	}

	m.data[key] = value

	// Run post-commit hooks
	for _, hook := range m.postCommitHooks {
		if err := hook(key, value); err != nil {
			return err
		}
	}

	return nil
}

// Delete removes a key from the map (not in a transaction).
// This will trigger pre-delete and post-delete hooks.
func (m *Map[K, V]) Delete(key K) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Run pre-delete hooks
	for _, hook := range m.preDeleteHooks {
		if err := hook(key); err != nil {
			return err
		}
	}

	delete(m.data, key)

	// Run post-delete hooks
	for _, hook := range m.postDeleteHooks {
		if err := hook(key); err != nil {
			return err
		}
	}

	return nil
}

// Len returns the number of items in the map.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// Keys returns all keys in the map.
func (m *Map[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]K, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

// Values returns all values in the map.
func (m *Map[K, V]) Values() []V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	values := make([]V, 0, len(m.data))
	for _, v := range m.data {
		values = append(values, v)
	}
	return values
}

// Clear removes all items from the map.
func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[K]V)
}

// operation represents a pending operation in a transaction.
type operation[K comparable, V any] struct {
	key      K
	value    V
	isDelete bool
}

// Transaction represents a transaction on the map with snapshot isolation.
type Transaction[K comparable, V any] struct {
	parent     *Map[K, V]
	snapshot   map[K]V
	pending    []operation[K, V]
	deleted    map[K]bool
	committed  bool
	rolledBack bool
	mu         sync.Mutex
}

// Begin starts a new transaction with a snapshot of the current map state.
func (m *Map[K, V]) Begin() *Transaction[K, V] {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make(map[K]V, len(m.data))
	for k, v := range m.data {
		snapshot[k] = v
	}

	return &Transaction[K, V]{
		parent:   m,
		snapshot: snapshot,
		pending:  make([]operation[K, V], 0),
		deleted:  make(map[K]bool),
	}
}

// Get retrieves a value from the transaction's view of the map.
// It first checks pending operations, then the snapshot.
func (t *Transaction[K, V]) Get(key K) (V, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var zero V
	if t.committed || t.rolledBack {
		return zero, false
	}

	// Check if the key was deleted in this transaction
	if t.deleted[key] {
		return zero, false
	}

	// Check pending operations in reverse order (most recent first)
	for i := len(t.pending) - 1; i >= 0; i-- {
		op := t.pending[i]
		if op.key == key {
			if op.isDelete {
				return zero, false
			}
			return op.value, true
		}
	}

	// Fall back to snapshot
	value, ok := t.snapshot[key]
	return value, ok
}

// Set stages a value to be set when the transaction is committed.
func (t *Transaction[K, V]) Set(key K, value V) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed || t.rolledBack {
		return ErrTransactionClosed
	}

	// Remove from deleted set if it was previously deleted in this transaction
	delete(t.deleted, key)

	t.pending = append(t.pending, operation[K, V]{
		key:      key,
		value:    value,
		isDelete: false,
	})
	return nil
}

// Delete stages a key to be deleted when the transaction is committed.
func (t *Transaction[K, V]) Delete(key K) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed || t.rolledBack {
		return ErrTransactionClosed
	}

	var zero V
	t.pending = append(t.pending, operation[K, V]{
		key:      key,
		value:    zero,
		isDelete: true,
	})
	t.deleted[key] = true
	return nil
}

// Commit applies all pending operations to the parent map.
// Pre-commit hooks are called before each operation, and post-commit hooks after.
// If any pre-commit hook fails, the entire transaction is aborted.
func (t *Transaction[K, V]) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed {
		return ErrTransactionClosed
	}
	if t.rolledBack {
		return ErrTransactionClosed
	}

	t.parent.mu.Lock()
	defer t.parent.mu.Unlock()

	// First, run all pre-commit hooks for all operations
	for _, op := range t.pending {
		if op.isDelete {
			for _, hook := range t.parent.preDeleteHooks {
				if err := hook(op.key); err != nil {
					return err
				}
			}
		} else {
			for _, hook := range t.parent.preCommitHooks {
				if err := hook(op.key, op.value); err != nil {
					return err
				}
			}
		}
	}

	// Apply all operations
	for _, op := range t.pending {
		if op.isDelete {
			delete(t.parent.data, op.key)
		} else {
			t.parent.data[op.key] = op.value
		}
	}

	// Run all post-commit hooks
	for _, op := range t.pending {
		if op.isDelete {
			for _, hook := range t.parent.postDeleteHooks {
				if err := hook(op.key); err != nil {
					// Note: The data is already committed at this point
					// We continue with other hooks but could log this error
					return err
				}
			}
		} else {
			for _, hook := range t.parent.postCommitHooks {
				if err := hook(op.key, op.value); err != nil {
					// Note: The data is already committed at this point
					return err
				}
			}
		}
	}

	t.committed = true
	return nil
}

// Rollback discards all pending operations.
func (t *Transaction[K, V]) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed {
		return ErrTransactionClosed
	}
	if t.rolledBack {
		return ErrTransactionClosed
	}

	t.pending = nil
	t.deleted = nil
	t.rolledBack = true
	return nil
}

// IsCommitted returns whether the transaction has been committed.
func (t *Transaction[K, V]) IsCommitted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.committed
}

// IsRolledBack returns whether the transaction has been rolled back.
func (t *Transaction[K, V]) IsRolledBack() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rolledBack
}

// IsClosed returns whether the transaction is closed (either committed or rolled back).
func (t *Transaction[K, V]) IsClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.committed || t.rolledBack
}

// Len returns the number of items visible in this transaction's view.
func (t *Transaction[K, V]) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed || t.rolledBack {
		return 0
	}

	// Start with snapshot
	count := len(t.snapshot)

	// Track keys that have been added or removed
	added := make(map[K]bool)
	removed := make(map[K]bool)

	for _, op := range t.pending {
		if op.isDelete {
			if _, existsInSnapshot := t.snapshot[op.key]; existsInSnapshot && !removed[op.key] {
				count--
				removed[op.key] = true
			} else if added[op.key] {
				count--
				delete(added, op.key)
			}
		} else {
			_, existsInSnapshot := t.snapshot[op.key]
			if !existsInSnapshot && !added[op.key] {
				count++
				added[op.key] = true
			}
			// If it was removed, add it back
			if removed[op.key] {
				count++
				delete(removed, op.key)
			}
		}
	}

	return count
}

// PendingChanges returns the number of pending operations in this transaction.
func (t *Transaction[K, V]) PendingChanges() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}
