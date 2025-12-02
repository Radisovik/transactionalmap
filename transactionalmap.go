// Package transactionalmap provides a generic map with transaction support,
// including pre-commit and post-commit hooks. The map is read-only and can
// only be modified through transactions, avoiding the need for snapshot copies.
package transactionalmap

import (
	"encoding/json"
	"errors"
	"sync"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// ErrKeyNotFound is returned when a key is not found in the map.
var ErrKeyNotFound = errors.New("key not found")

// ErrTransactionClosed is returned when trying to use a closed transaction.
var ErrTransactionClosed = errors.New("transaction is closed")

// PreCommitHookFunc is the type for pre-commit hook functions.
// It receives all pending changes (writes and deletes as zero values) and can
// modify the map to polish/modify items before they are committed.
// Return an error to abort the commit.
type PreCommitHookFunc[K comparable, V any] func(changes map[K]V) error

// PostCommitHookFunc is the type for post-commit hook functions.
// It receives all changes that were applied (writes and deletes as zero values).
// This is useful for notifications such as emitting a JSON merge patch.
type PostCommitHookFunc[K comparable, V any] func(changes map[K]V)

// Map is a generic map that supports transactions. The map is read-only
// and can only be modified through transactions, which avoids the need
// for making snapshot copies when beginning a transaction.
type Map[K comparable, V any] struct {
	mu              sync.RWMutex
	data            map[K]V
	preCommitHooks  []PreCommitHookFunc[K, V]
	postCommitHooks []PostCommitHookFunc[K, V]
}

// New creates a new transactional map.
func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		data:            make(map[K]V),
		preCommitHooks:  make([]PreCommitHookFunc[K, V], 0),
		postCommitHooks: make([]PostCommitHookFunc[K, V], 0),
	}
}

// AddPreCommitHook adds a hook that will be called before changes are committed.
// The hook receives all pending changes and can modify them to polish/modify items
// before they are committed. If the hook returns an error, the commit will be aborted.
func (m *Map[K, V]) AddPreCommitHook(hook PreCommitHookFunc[K, V]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preCommitHooks = append(m.preCommitHooks, hook)
}

// AddPostCommitHook adds a hook that will be called after changes are committed.
// The hook receives all changes that were applied. This is useful for notifications
// such as emitting a JSON merge patch.
func (m *Map[K, V]) AddPostCommitHook(hook PostCommitHookFunc[K, V]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postCommitHooks = append(m.postCommitHooks, hook)
}

// Get retrieves a value by key directly from the map.
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.data[key]
	return value, ok
}

// Len returns the number of items in the map.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// Range calls the provided function for each key-value pair in the map.
// If the function returns false, iteration stops.
func (m *Map[K, V]) Range(fn func(key K, value V) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data {
		if !fn(k, v) {
			return
		}
	}
}

// Transaction represents a transaction on the map. Since the map is read-only,
// transactions read directly from the parent map and overlay pending changes,
// avoiding the need for snapshot copies.
type Transaction[K comparable, V any] struct {
	parent     *Map[K, V]
	changes    map[K]V    // pending changes (writes and deletes as zero values)
	deleted    map[K]bool // track which keys are marked for deletion
	committed  bool
	rolledBack bool
	mu         sync.Mutex
}

// Begin starts a new transaction. Since the map is read-only (can only be
// modified through transactions), no snapshot copy is needed.
func (m *Map[K, V]) Begin() *Transaction[K, V] {
	return &Transaction[K, V]{
		parent:  m,
		changes: make(map[K]V),
		deleted: make(map[K]bool),
	}
}

// hasChanged checks if the new value is different from the current value using JSON merge patch.
// Returns true if the value has changed, false if it's identical.
// Uses RFC 7396 JSON Merge Patch - if the patch is empty {}, there's no change.
func hasChanged[V any](oldValue, newValue V) bool {
	oldJSON, err := json.Marshal(oldValue)
	if err != nil {
		return true
	}

	newJSON, err := json.Marshal(newValue)
	if err != nil {
		return true
	}

	patch, err := jsonpatch.CreateMergePatch(oldJSON, newJSON)
	if err != nil {
		return true
	}

	// Empty patch {} means no change
	return string(patch) != "{}"
}

// Get retrieves a value from the transaction's view of the map.
// It first checks pending changes, then reads from the parent map.
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

	// Check pending changes
	if value, ok := t.changes[key]; ok {
		return value, true
	}

	// Read from parent map
	t.parent.mu.RLock()
	defer t.parent.mu.RUnlock()
	value, ok := t.parent.data[key]
	return value, ok
}

// Set stages a value to be set when the transaction is committed.
// If the value is identical to the current value (determined by JSON merge patch),
// the change is not recorded.
func (t *Transaction[K, V]) Set(key K, value V) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed || t.rolledBack {
		return ErrTransactionClosed
	}

	// Get current value (either from pending changes or parent)
	var currentValue V
	var hasCurrentValue bool

	if t.deleted[key] {
		// Key was deleted in this transaction, so setting it is a change
		hasCurrentValue = false
	} else if val, ok := t.changes[key]; ok {
		currentValue = val
		hasCurrentValue = true
	} else {
		t.parent.mu.RLock()
		currentValue, hasCurrentValue = t.parent.data[key]
		t.parent.mu.RUnlock()
	}

	// Check if value actually changed
	if hasCurrentValue && !hasChanged(currentValue, value) {
		return nil
	}

	// Remove from deleted set if it was previously deleted
	delete(t.deleted, key)

	// Store in pending changes
	t.changes[key] = value
	return nil
}

// Delete stages a key to be deleted when the transaction is committed.
// Deleted keys are represented in the changes map with the zero value.
func (t *Transaction[K, V]) Delete(key K) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed || t.rolledBack {
		return ErrTransactionClosed
	}

	// Check if key exists (either in pending changes or parent)
	keyExists := false
	if _, ok := t.changes[key]; ok && !t.deleted[key] {
		keyExists = true
	} else if !t.deleted[key] {
		t.parent.mu.RLock()
		_, keyExists = t.parent.data[key]
		t.parent.mu.RUnlock()
	}

	// If key doesn't exist or already deleted, no change needed
	if !keyExists {
		return nil
	}

	// Mark as deleted and store zero value in changes
	var zero V
	t.changes[key] = zero
	t.deleted[key] = true
	return nil
}

// Commit applies all pending operations to the parent map.
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

	// Run all pre-commit hooks - they can modify the changes
	for _, hook := range t.parent.preCommitHooks {
		if err := hook(t.changes); err != nil {
			return err
		}
	}

	// Apply all changes
	for key, value := range t.changes {
		if t.deleted[key] {
			delete(t.parent.data, key)
		} else {
			t.parent.data[key] = value
		}
	}

	t.committed = true

	// Run all post-commit hooks for notifications
	if len(t.parent.postCommitHooks) > 0 {
		finalChanges := make(map[K]V, len(t.changes))
		for k, v := range t.changes {
			finalChanges[k] = v
		}
		for _, hook := range t.parent.postCommitHooks {
			hook(finalChanges)
		}
	}

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

	t.changes = nil
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

	t.parent.mu.RLock()
	defer t.parent.mu.RUnlock()

	count := len(t.parent.data)

	for key := range t.changes {
		_, existsInParent := t.parent.data[key]
		if t.deleted[key] {
			if existsInParent {
				count--
			}
		} else {
			if !existsInParent {
				count++
			}
		}
	}

	return count
}

// PendingChanges returns the number of pending changes in this transaction.
func (t *Transaction[K, V]) PendingChanges() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.changes)
}

// Range calls the provided function for each key-value pair visible in this transaction.
// If the function returns false, iteration stops.
func (t *Transaction[K, V]) Range(fn func(key K, value V) bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.committed || t.rolledBack {
		return
	}

	t.parent.mu.RLock()
	defer t.parent.mu.RUnlock()

	visited := make(map[K]bool)

	// First, iterate over changes (excluding deletes)
	for key, value := range t.changes {
		visited[key] = true
		if !t.deleted[key] {
			if !fn(key, value) {
				return
			}
		}
	}

	// Then, iterate over parent map (excluding visited and deleted keys)
	for key, value := range t.parent.data {
		if !visited[key] && !t.deleted[key] {
			if !fn(key, value) {
				return
			}
		}
	}
}
