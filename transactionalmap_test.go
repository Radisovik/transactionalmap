package transactionalmap

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestNew tests the creation of a new transactional map.
func TestNew(t *testing.T) {
	m := New[string, int]()

	if m == nil {
		t.Fatal("New() returned nil")
	}

	if m.Len() != 0 {
		t.Errorf("New map should be empty, got length %d", m.Len())
	}
}

// TestBasicSetGet tests basic set and get operations.
func TestBasicSetGet(t *testing.T) {
	m := New[string, int]()

	// Set a value
	err := m.Set("key1", 100)
	if err != nil {
		t.Fatalf("Set() returned error: %v", err)
	}

	// Get the value
	value, ok := m.Get("key1")
	if !ok {
		t.Fatal("Get() returned false for existing key")
	}
	if value != 100 {
		t.Errorf("Get() = %d, want 100", value)
	}

	// Get non-existent key
	_, ok = m.Get("nonexistent")
	if ok {
		t.Error("Get() returned true for non-existent key")
	}
}

// TestDelete tests the delete operation.
func TestDelete(t *testing.T) {
	m := New[string, string]()

	_ = m.Set("key1", "value1")
	_ = m.Set("key2", "value2")

	// Delete key1
	err := m.Delete("key1")
	if err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	// Verify key1 is deleted
	_, ok := m.Get("key1")
	if ok {
		t.Error("Get() returned true for deleted key")
	}

	// Verify key2 still exists
	value, ok := m.Get("key2")
	if !ok {
		t.Fatal("key2 should still exist")
	}
	if value != "value2" {
		t.Errorf("Get(key2) = %s, want value2", value)
	}

	// Verify length
	if m.Len() != 1 {
		t.Errorf("Len() = %d, want 1", m.Len())
	}
}

// TestLen tests the Len method.
func TestLen(t *testing.T) {
	m := New[int, string]()

	if m.Len() != 0 {
		t.Errorf("Empty map Len() = %d, want 0", m.Len())
	}

	_ = m.Set(1, "one")
	if m.Len() != 1 {
		t.Errorf("After one Set, Len() = %d, want 1", m.Len())
	}

	_ = m.Set(2, "two")
	_ = m.Set(3, "three")
	if m.Len() != 3 {
		t.Errorf("After three Sets, Len() = %d, want 3", m.Len())
	}

	_ = m.Delete(2)
	if m.Len() != 2 {
		t.Errorf("After Delete, Len() = %d, want 2", m.Len())
	}
}

// TestKeys tests the Keys method.
func TestKeys(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("a", 1)
	_ = m.Set("b", 2)
	_ = m.Set("c", 3)

	keys := m.Keys()

	if len(keys) != 3 {
		t.Errorf("Keys() length = %d, want 3", len(keys))
	}

	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}

	for _, expected := range []string{"a", "b", "c"} {
		if !keySet[expected] {
			t.Errorf("Keys() missing key %s", expected)
		}
	}
}

// TestValues tests the Values method.
func TestValues(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("a", 1)
	_ = m.Set("b", 2)
	_ = m.Set("c", 3)

	values := m.Values()

	if len(values) != 3 {
		t.Errorf("Values() length = %d, want 3", len(values))
	}

	valueSet := make(map[int]bool)
	for _, v := range values {
		valueSet[v] = true
	}

	for _, expected := range []int{1, 2, 3} {
		if !valueSet[expected] {
			t.Errorf("Values() missing value %d", expected)
		}
	}
}

// TestClear tests the Clear method.
func TestClear(t *testing.T) {
	m := New[string, string]()

	_ = m.Set("key1", "value1")
	_ = m.Set("key2", "value2")

	if m.Len() != 2 {
		t.Fatalf("Before Clear, Len() = %d, want 2", m.Len())
	}

	m.Clear()

	if m.Len() != 0 {
		t.Errorf("After Clear, Len() = %d, want 0", m.Len())
	}

	_, ok := m.Get("key1")
	if ok {
		t.Error("After Clear, Get() returned true for key1")
	}
}

// TestTransactionBasic tests basic transaction operations.
func TestTransactionBasic(t *testing.T) {
	m := New[string, int]()

	// Set some initial data
	_ = m.Set("existing", 1)

	// Begin transaction
	tx := m.Begin()

	if tx == nil {
		t.Fatal("Begin() returned nil")
	}

	// Read existing value through transaction
	value, ok := tx.Get("existing")
	if !ok {
		t.Fatal("Transaction Get() returned false for existing key")
	}
	if value != 1 {
		t.Errorf("Transaction Get() = %d, want 1", value)
	}

	// Set new value in transaction
	err := tx.Set("new", 2)
	if err != nil {
		t.Fatalf("Transaction Set() returned error: %v", err)
	}

	// Read new value through transaction
	value, ok = tx.Get("new")
	if !ok {
		t.Fatal("Transaction Get() returned false for new key")
	}
	if value != 2 {
		t.Errorf("Transaction Get() = %d, want 2", value)
	}

	// Verify new value is not yet in parent map
	_, ok = m.Get("new")
	if ok {
		t.Error("New key should not be visible in parent map before commit")
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}

	// Verify new value is now in parent map
	value, ok = m.Get("new")
	if !ok {
		t.Fatal("After commit, Get() returned false for new key")
	}
	if value != 2 {
		t.Errorf("After commit, Get() = %d, want 2", value)
	}
}

// TestTransactionIsolation tests that transactions provide snapshot isolation.
func TestTransactionIsolation(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("key1", 10)
	_ = m.Set("key2", 20)

	// Begin transaction 1
	tx1 := m.Begin()

	// Modify map directly after tx1 started
	_ = m.Set("key1", 100)
	_ = m.Set("key3", 30)

	// tx1 should still see the old values (snapshot isolation)
	value, ok := tx1.Get("key1")
	if !ok {
		t.Fatal("tx1 should see key1")
	}
	if value != 10 {
		t.Errorf("tx1 sees key1 = %d, want 10 (snapshot)", value)
	}

	// tx1 should not see key3 which was added after snapshot
	_, ok = tx1.Get("key3")
	if ok {
		t.Error("tx1 should not see key3 (added after snapshot)")
	}

	// Begin transaction 2 (should see current state)
	tx2 := m.Begin()

	value, ok = tx2.Get("key1")
	if !ok {
		t.Fatal("tx2 should see key1")
	}
	if value != 100 {
		t.Errorf("tx2 sees key1 = %d, want 100", value)
	}

	value, ok = tx2.Get("key3")
	if !ok {
		t.Fatal("tx2 should see key3")
	}
	if value != 30 {
		t.Errorf("tx2 sees key3 = %d, want 30", value)
	}
}

// TestTransactionDelete tests deleting keys in a transaction.
func TestTransactionDelete(t *testing.T) {
	m := New[string, string]()

	_ = m.Set("keep", "value1")
	_ = m.Set("delete", "value2")

	tx := m.Begin()

	// Delete a key in the transaction
	err := tx.Delete("delete")
	if err != nil {
		t.Fatalf("Transaction Delete() returned error: %v", err)
	}

	// Verify key is deleted from transaction's view
	_, ok := tx.Get("delete")
	if ok {
		t.Error("Deleted key should not be visible in transaction")
	}

	// Verify key still exists in parent map
	_, ok = m.Get("delete")
	if !ok {
		t.Error("Deleted key should still exist in parent map before commit")
	}

	// Commit and verify
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}

	_, ok = m.Get("delete")
	if ok {
		t.Error("Deleted key should not exist in parent map after commit")
	}

	// Verify other key still exists
	_, ok = m.Get("keep")
	if !ok {
		t.Error("keep key should still exist")
	}
}

// TestTransactionRollback tests rolling back a transaction.
func TestTransactionRollback(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("original", 1)

	tx := m.Begin()

	// Make some changes
	_ = tx.Set("new", 2)
	_ = tx.Set("original", 100)
	_ = tx.Delete("original")

	// Rollback
	err := tx.Rollback()
	if err != nil {
		t.Fatalf("Rollback() returned error: %v", err)
	}

	// Verify changes were not applied
	_, ok := m.Get("new")
	if ok {
		t.Error("new key should not exist after rollback")
	}

	value, ok := m.Get("original")
	if !ok {
		t.Fatal("original key should still exist after rollback")
	}
	if value != 1 {
		t.Errorf("original = %d, want 1 (unchanged)", value)
	}
}

// TestTransactionClosedError tests that operations fail on closed transactions.
func TestTransactionClosedError(t *testing.T) {
	m := New[string, int]()

	// Test after commit
	tx := m.Begin()
	_ = tx.Commit()

	err := tx.Set("key", 1)
	if err != ErrTransactionClosed {
		t.Errorf("Set after commit: got %v, want ErrTransactionClosed", err)
	}

	err = tx.Delete("key")
	if err != ErrTransactionClosed {
		t.Errorf("Delete after commit: got %v, want ErrTransactionClosed", err)
	}

	err = tx.Commit()
	if err != ErrTransactionClosed {
		t.Errorf("Double commit: got %v, want ErrTransactionClosed", err)
	}

	// Test after rollback
	tx2 := m.Begin()
	_ = tx2.Rollback()

	err = tx2.Set("key", 1)
	if err != ErrTransactionClosed {
		t.Errorf("Set after rollback: got %v, want ErrTransactionClosed", err)
	}

	err = tx2.Rollback()
	if err != ErrTransactionClosed {
		t.Errorf("Double rollback: got %v, want ErrTransactionClosed", err)
	}
}

// TestPreCommitHook tests pre-commit hooks.
func TestPreCommitHook(t *testing.T) {
	m := New[string, int]()

	hookCalled := false
	var hookedKey string
	var hookedValue int

	m.AddPreCommitHook(func(key string, value int) error {
		hookCalled = true
		hookedKey = key
		hookedValue = value
		return nil
	})

	err := m.Set("testkey", 42)
	if err != nil {
		t.Fatalf("Set() returned error: %v", err)
	}

	if !hookCalled {
		t.Error("Pre-commit hook was not called")
	}
	if hookedKey != "testkey" {
		t.Errorf("Hook received key %s, want testkey", hookedKey)
	}
	if hookedValue != 42 {
		t.Errorf("Hook received value %d, want 42", hookedValue)
	}
}

// TestPreCommitHookError tests that pre-commit hook errors abort the operation.
func TestPreCommitHookError(t *testing.T) {
	m := New[string, int]()

	expectedErr := errors.New("validation failed")

	m.AddPreCommitHook(func(key string, value int) error {
		if value < 0 {
			return expectedErr
		}
		return nil
	})

	// Positive value should succeed
	err := m.Set("positive", 10)
	if err != nil {
		t.Fatalf("Set() with positive value returned error: %v", err)
	}

	// Negative value should fail
	err = m.Set("negative", -5)
	if err != expectedErr {
		t.Errorf("Set() with negative value: got %v, want %v", err, expectedErr)
	}

	// Verify the negative value was not set
	_, ok := m.Get("negative")
	if ok {
		t.Error("Negative value should not have been set")
	}
}

// TestPostCommitHook tests post-commit hooks.
func TestPostCommitHook(t *testing.T) {
	m := New[string, int]()

	var hookCalls []string

	m.AddPostCommitHook(func(key string, value int) error {
		hookCalls = append(hookCalls, key)
		return nil
	})

	_ = m.Set("first", 1)
	_ = m.Set("second", 2)

	if len(hookCalls) != 2 {
		t.Errorf("Post-commit hook called %d times, want 2", len(hookCalls))
	}
}

// TestPreDeleteHook tests pre-delete hooks.
func TestPreDeleteHook(t *testing.T) {
	m := New[string, string]()

	_ = m.Set("deletable", "value1")
	_ = m.Set("protected", "value2")

	m.AddPreDeleteHook(func(key string) error {
		if key == "protected" {
			return errors.New("cannot delete protected key")
		}
		return nil
	})

	// Deleting deletable should succeed
	err := m.Delete("deletable")
	if err != nil {
		t.Fatalf("Delete(deletable) returned error: %v", err)
	}

	// Deleting protected should fail
	err = m.Delete("protected")
	if err == nil {
		t.Error("Delete(protected) should have returned error")
	}

	// Verify protected still exists
	_, ok := m.Get("protected")
	if !ok {
		t.Error("protected key should still exist")
	}
}

// TestPostDeleteHook tests post-delete hooks.
func TestPostDeleteHook(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("key1", 1)
	_ = m.Set("key2", 2)

	var deletedKeys []string

	m.AddPostDeleteHook(func(key string) error {
		deletedKeys = append(deletedKeys, key)
		return nil
	})

	_ = m.Delete("key1")
	_ = m.Delete("key2")

	if len(deletedKeys) != 2 {
		t.Errorf("Post-delete hook called %d times, want 2", len(deletedKeys))
	}
}

// TestTransactionHooks tests that hooks are called during transaction commit.
func TestTransactionHooks(t *testing.T) {
	m := New[string, int]()

	var preCommitCalls []string
	var postCommitCalls []string
	var preDeleteCalls []string
	var postDeleteCalls []string

	m.AddPreCommitHook(func(key string, value int) error {
		preCommitCalls = append(preCommitCalls, key)
		return nil
	})
	m.AddPostCommitHook(func(key string, value int) error {
		postCommitCalls = append(postCommitCalls, key)
		return nil
	})
	m.AddPreDeleteHook(func(key string) error {
		preDeleteCalls = append(preDeleteCalls, key)
		return nil
	})
	m.AddPostDeleteHook(func(key string) error {
		postDeleteCalls = append(postDeleteCalls, key)
		return nil
	})

	// Set up initial data
	_ = m.Set("existing", 1)

	// Clear the calls from setup
	preCommitCalls = nil
	postCommitCalls = nil

	tx := m.Begin()
	_ = tx.Set("new1", 10)
	_ = tx.Set("new2", 20)
	_ = tx.Delete("existing")

	// Hooks should not be called yet
	if len(preCommitCalls) > 0 || len(postCommitCalls) > 0 {
		t.Error("Hooks should not be called before commit")
	}

	err := tx.Commit()
	if err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}

	// Verify pre-commit hooks were called
	if len(preCommitCalls) != 2 {
		t.Errorf("Pre-commit hook called %d times, want 2", len(preCommitCalls))
	}

	// Verify post-commit hooks were called
	if len(postCommitCalls) != 2 {
		t.Errorf("Post-commit hook called %d times, want 2", len(postCommitCalls))
	}

	// Verify pre-delete hooks were called
	if len(preDeleteCalls) != 1 {
		t.Errorf("Pre-delete hook called %d times, want 1", len(preDeleteCalls))
	}

	// Verify post-delete hooks were called
	if len(postDeleteCalls) != 1 {
		t.Errorf("Post-delete hook called %d times, want 1", len(postDeleteCalls))
	}
}

// TestTransactionPreCommitHookError tests that pre-commit hook errors abort transaction.
func TestTransactionPreCommitHookError(t *testing.T) {
	m := New[string, int]()

	expectedErr := errors.New("validation failed")

	m.AddPreCommitHook(func(key string, value int) error {
		if key == "invalid" {
			return expectedErr
		}
		return nil
	})

	_ = m.Set("original", 1)

	tx := m.Begin()
	_ = tx.Set("valid", 10)
	_ = tx.Set("invalid", 20)
	_ = tx.Set("another", 30)

	err := tx.Commit()
	if err != expectedErr {
		t.Errorf("Commit() error = %v, want %v", err, expectedErr)
	}

	// Verify none of the transaction changes were applied
	// (atomicity - if one fails, all fail)
	_, ok := m.Get("valid")
	if ok {
		t.Error("valid key should not exist after failed commit")
	}

	_, ok = m.Get("another")
	if ok {
		t.Error("another key should not exist after failed commit")
	}

	// Original should still exist
	_, ok = m.Get("original")
	if !ok {
		t.Error("original key should still exist")
	}
}

// TestMultipleHooks tests that multiple hooks are called in order.
func TestMultipleHooks(t *testing.T) {
	m := New[string, int]()

	var order []int

	m.AddPreCommitHook(func(key string, value int) error {
		order = append(order, 1)
		return nil
	})
	m.AddPreCommitHook(func(key string, value int) error {
		order = append(order, 2)
		return nil
	})
	m.AddPostCommitHook(func(key string, value int) error {
		order = append(order, 3)
		return nil
	})
	m.AddPostCommitHook(func(key string, value int) error {
		order = append(order, 4)
		return nil
	})

	_ = m.Set("key", 1)

	expected := []int{1, 2, 3, 4}
	if len(order) != len(expected) {
		t.Fatalf("Expected %d hook calls, got %d", len(expected), len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("Hook order[%d] = %d, want %d", i, order[i], v)
		}
	}
}

// TestTransactionLen tests the Len method on transactions.
func TestTransactionLen(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("a", 1)
	_ = m.Set("b", 2)

	tx := m.Begin()

	if tx.Len() != 2 {
		t.Errorf("Initial transaction Len() = %d, want 2", tx.Len())
	}

	_ = tx.Set("c", 3)
	if tx.Len() != 3 {
		t.Errorf("After adding key, Len() = %d, want 3", tx.Len())
	}

	_ = tx.Delete("a")
	if tx.Len() != 2 {
		t.Errorf("After deleting key, Len() = %d, want 2", tx.Len())
	}

	_ = tx.Set("d", 4)
	_ = tx.Set("e", 5)
	if tx.Len() != 4 {
		t.Errorf("After more adds, Len() = %d, want 4", tx.Len())
	}
}

// TestTransactionPendingChanges tests the PendingChanges method.
func TestTransactionPendingChanges(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("existing", 1)

	tx := m.Begin()

	if tx.PendingChanges() != 0 {
		t.Errorf("Initial PendingChanges() = %d, want 0", tx.PendingChanges())
	}

	_ = tx.Set("new1", 10)
	if tx.PendingChanges() != 1 {
		t.Errorf("After 1 Set, PendingChanges() = %d, want 1", tx.PendingChanges())
	}

	_ = tx.Set("new2", 20)
	_ = tx.Delete("existing")
	if tx.PendingChanges() != 3 {
		t.Errorf("After 2 Sets and 1 Delete, PendingChanges() = %d, want 3", tx.PendingChanges())
	}
}

// TestTransactionIsClosedMethods tests IsCommitted, IsRolledBack, and IsClosed.
func TestTransactionIsClosedMethods(t *testing.T) {
	m := New[string, int]()

	// Test commit
	tx1 := m.Begin()
	if tx1.IsCommitted() || tx1.IsRolledBack() || tx1.IsClosed() {
		t.Error("New transaction should not be closed")
	}

	_ = tx1.Commit()
	if !tx1.IsCommitted() {
		t.Error("After Commit(), IsCommitted() should be true")
	}
	if tx1.IsRolledBack() {
		t.Error("After Commit(), IsRolledBack() should be false")
	}
	if !tx1.IsClosed() {
		t.Error("After Commit(), IsClosed() should be true")
	}

	// Test rollback
	tx2 := m.Begin()
	_ = tx2.Rollback()
	if tx2.IsCommitted() {
		t.Error("After Rollback(), IsCommitted() should be false")
	}
	if !tx2.IsRolledBack() {
		t.Error("After Rollback(), IsRolledBack() should be true")
	}
	if !tx2.IsClosed() {
		t.Error("After Rollback(), IsClosed() should be true")
	}
}

// TestConcurrentReads tests concurrent read operations.
func TestConcurrentReads(t *testing.T) {
	m := New[int, int]()

	// Set up initial data
	for i := 0; i < 100; i++ {
		_ = m.Set(i, i*10)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Spawn multiple goroutines reading concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(key int) {
			defer wg.Done()
			value, ok := m.Get(key)
			if !ok {
				errChan <- errors.New("key not found")
				return
			}
			if value != key*10 {
				errChan <- errors.New("incorrect value")
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}
}

// TestConcurrentWrites tests concurrent write operations.
func TestConcurrentWrites(t *testing.T) {
	m := New[int, int]()

	var wg sync.WaitGroup
	iterations := 100

	// Spawn multiple goroutines writing concurrently
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(key int) {
			defer wg.Done()
			_ = m.Set(key, key*10)
		}(i)
	}

	wg.Wait()

	// Verify all writes succeeded
	if m.Len() != iterations {
		t.Errorf("Len() = %d, want %d", m.Len(), iterations)
	}

	for i := 0; i < iterations; i++ {
		value, ok := m.Get(i)
		if !ok {
			t.Errorf("Key %d not found", i)
			continue
		}
		if value != i*10 {
			t.Errorf("Get(%d) = %d, want %d", i, value, i*10)
		}
	}
}

// TestConcurrentTransactions tests multiple concurrent transactions.
func TestConcurrentTransactions(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("counter", 0)

	var wg sync.WaitGroup
	transactions := 10
	incrementsPerTx := 10

	var successfulCommits atomic.Int32

	for i := 0; i < transactions; i++ {
		wg.Add(1)
		go func(txNum int) {
			defer wg.Done()

			for j := 0; j < incrementsPerTx; j++ {
				tx := m.Begin()
				key := txNum*incrementsPerTx + j
				_ = tx.Set(string(rune('a'+key%26))+string(rune('0'+key)), key)
				err := tx.Commit()
				if err == nil {
					successfulCommits.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Successful commits: %d", successfulCommits.Load())
}

// TestGenericTypes tests the map with various generic type combinations.
func TestGenericTypes(t *testing.T) {
	t.Run("StringToStruct", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}

		m := New[string, Person]()
		_ = m.Set("alice", Person{Name: "Alice", Age: 30})

		value, ok := m.Get("alice")
		if !ok {
			t.Fatal("Get() returned false")
		}
		if value.Name != "Alice" || value.Age != 30 {
			t.Errorf("Got %+v, want {Name:Alice Age:30}", value)
		}
	})

	t.Run("IntToSlice", func(t *testing.T) {
		m := New[int, []string]()
		_ = m.Set(1, []string{"a", "b", "c"})

		value, ok := m.Get(1)
		if !ok {
			t.Fatal("Get() returned false")
		}
		if len(value) != 3 || value[0] != "a" {
			t.Errorf("Got %v, want [a b c]", value)
		}
	})

	t.Run("StructToMap", func(t *testing.T) {
		type Key struct {
			X, Y int
		}

		m := New[Key, map[string]int]()
		_ = m.Set(Key{1, 2}, map[string]int{"value": 42})

		value, ok := m.Get(Key{1, 2})
		if !ok {
			t.Fatal("Get() returned false")
		}
		if value["value"] != 42 {
			t.Errorf("Got %v, want map[value:42]", value)
		}
	})
}

// TestTransactionOverwrite tests overwriting values in a transaction.
func TestTransactionOverwrite(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("key", 1)

	tx := m.Begin()

	// Overwrite multiple times in the same transaction
	_ = tx.Set("key", 10)
	_ = tx.Set("key", 20)
	_ = tx.Set("key", 30)

	value, ok := tx.Get("key")
	if !ok {
		t.Fatal("Get() returned false")
	}
	if value != 30 {
		t.Errorf("Get() = %d, want 30 (last set value)", value)
	}

	_ = tx.Commit()

	value, ok = m.Get("key")
	if !ok {
		t.Fatal("After commit, Get() returned false")
	}
	if value != 30 {
		t.Errorf("After commit, Get() = %d, want 30", value)
	}
}

// TestTransactionDeleteThenSet tests deleting then re-setting a key.
func TestTransactionDeleteThenSet(t *testing.T) {
	m := New[string, int]()

	_ = m.Set("key", 1)

	tx := m.Begin()

	_ = tx.Delete("key")
	_, ok := tx.Get("key")
	if ok {
		t.Error("After Delete, Get() should return false")
	}

	_ = tx.Set("key", 100)
	value, ok := tx.Get("key")
	if !ok {
		t.Fatal("After Set, Get() should return true")
	}
	if value != 100 {
		t.Errorf("Get() = %d, want 100", value)
	}

	_ = tx.Commit()

	value, ok = m.Get("key")
	if !ok {
		t.Fatal("After commit, Get() should return true")
	}
	if value != 100 {
		t.Errorf("After commit, Get() = %d, want 100", value)
	}
}

// TestTransactionSetThenDelete tests setting then deleting a key.
func TestTransactionSetThenDelete(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()

	_ = tx.Set("key", 100)
	value, ok := tx.Get("key")
	if !ok {
		t.Fatal("After Set, Get() should return true")
	}
	if value != 100 {
		t.Errorf("Get() = %d, want 100", value)
	}

	_ = tx.Delete("key")
	_, ok = tx.Get("key")
	if ok {
		t.Error("After Delete, Get() should return false")
	}

	_ = tx.Commit()

	_, ok = m.Get("key")
	if ok {
		t.Error("After commit of deleted key, Get() should return false")
	}
}

// TestGetAfterClosedTransaction tests Get behavior after transaction closure.
func TestGetAfterClosedTransaction(t *testing.T) {
	m := New[string, int]()
	_ = m.Set("key", 42)

	// After commit
	tx1 := m.Begin()
	_ = tx1.Set("newkey", 100)
	_ = tx1.Commit()

	_, ok := tx1.Get("key")
	if ok {
		t.Error("Get() on committed transaction should return false")
	}

	// After rollback
	tx2 := m.Begin()
	_ = tx2.Set("anotherkey", 200)
	_ = tx2.Rollback()

	_, ok = tx2.Get("key")
	if ok {
		t.Error("Get() on rolled back transaction should return false")
	}
}

// TestEmptyTransaction tests committing an empty transaction.
func TestEmptyTransaction(t *testing.T) {
	m := New[string, int]()
	_ = m.Set("key", 1)

	tx := m.Begin()

	// Commit without any changes
	err := tx.Commit()
	if err != nil {
		t.Fatalf("Commit() of empty transaction returned error: %v", err)
	}

	// Verify map is unchanged
	value, ok := m.Get("key")
	if !ok || value != 1 {
		t.Error("Map should be unchanged after empty transaction commit")
	}
}

// TestLenAfterClosedTransaction tests Len behavior after transaction closure.
func TestLenAfterClosedTransaction(t *testing.T) {
	m := New[string, int]()
	_ = m.Set("a", 1)
	_ = m.Set("b", 2)

	tx := m.Begin()
	_ = tx.Set("c", 3)
	_ = tx.Commit()

	if tx.Len() != 0 {
		t.Errorf("Len() on committed transaction = %d, want 0", tx.Len())
	}

	tx2 := m.Begin()
	_ = tx2.Set("d", 4)
	_ = tx2.Rollback()

	if tx2.Len() != 0 {
		t.Errorf("Len() on rolled back transaction = %d, want 0", tx2.Len())
	}
}

// TestDeleteNonExistentKey tests deleting a key that doesn't exist.
func TestDeleteNonExistentKey(t *testing.T) {
	m := New[string, int]()

	// Direct delete of non-existent key
	err := m.Delete("nonexistent")
	if err != nil {
		t.Errorf("Delete(nonexistent) returned error: %v", err)
	}

	// Transaction delete of non-existent key
	tx := m.Begin()
	err = tx.Delete("nonexistent")
	if err != nil {
		t.Errorf("Transaction Delete(nonexistent) returned error: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		t.Errorf("Commit after deleting nonexistent key returned error: %v", err)
	}
}

// BenchmarkSet benchmarks the Set operation.
func BenchmarkSet(b *testing.B) {
	m := New[int, int]()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Set(i, i)
	}
}

// BenchmarkGet benchmarks the Get operation.
func BenchmarkGet(b *testing.B) {
	m := New[int, int]()
	for i := 0; i < 1000; i++ {
		_ = m.Set(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Get(i % 1000)
	}
}

// BenchmarkTransactionCommit benchmarks transaction commit.
func BenchmarkTransactionCommit(b *testing.B) {
	m := New[int, int]()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := m.Begin()
		_ = tx.Set(i, i)
		_ = tx.Commit()
	}
}

// BenchmarkTransactionWithHooks benchmarks transactions with hooks.
func BenchmarkTransactionWithHooks(b *testing.B) {
	m := New[int, int]()
	m.AddPreCommitHook(func(k int, v int) error { return nil })
	m.AddPostCommitHook(func(k int, v int) error { return nil })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := m.Begin()
		_ = tx.Set(i, i)
		_ = tx.Commit()
	}
}
