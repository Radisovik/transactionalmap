package transactionalmap

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestNew tests the creation of a new transactional map.
func TestNewMap(t *testing.T) {
	m := New[string, int]()

	if m == nil {
		t.Fatal("New() returned nil")
	}

	if m.Len() != 0 {
		t.Errorf("New map should be empty, got length %d", m.Len())
	}
}

// TestBasicSetGetViaTransaction tests basic set and get operations via transactions.
func TestBasicSetGetViaTransaction(t *testing.T) {
	m := New[string, int]()

	// Set a value via transaction
	tx := m.Begin()
	err := tx.Set("key1", 100)
	if err != nil {
		t.Fatalf("Set() returned error: %v", err)
	}
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Commit() returned error: %v", err)
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

// TestDeleteViaTransaction tests the delete operation via transaction.
func TestDeleteViaTransaction(t *testing.T) {
	m := New[string, string]()

	// Set initial values via transaction
	tx := m.Begin()
	_ = tx.Set("key1", "value1")
	_ = tx.Set("key2", "value2")
	_ = tx.Commit()

	// Delete key1 via transaction
	tx = m.Begin()
	err := tx.Delete("key1")
	if err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Commit() returned error: %v", err)
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

// TestLenViaTransaction tests the Len method.
func TestLenViaTransaction(t *testing.T) {
	m := New[int, string]()

	if m.Len() != 0 {
		t.Errorf("Empty map Len() = %d, want 0", m.Len())
	}

	tx := m.Begin()
	_ = tx.Set(1, "one")
	_ = tx.Commit()
	if m.Len() != 1 {
		t.Errorf("After one Set, Len() = %d, want 1", m.Len())
	}

	tx = m.Begin()
	_ = tx.Set(2, "two")
	_ = tx.Set(3, "three")
	_ = tx.Commit()
	if m.Len() != 3 {
		t.Errorf("After three Sets, Len() = %d, want 3", m.Len())
	}

	tx = m.Begin()
	_ = tx.Delete(2)
	_ = tx.Commit()
	if m.Len() != 2 {
		t.Errorf("After Delete, Len() = %d, want 2", m.Len())
	}
}

// TestRange tests the Range method on the map.
func TestRange(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("a", 1)
	_ = tx.Set("b", 2)
	_ = tx.Set("c", 3)
	_ = tx.Commit()

	collected := make(map[string]int)
	m.Range(func(key string, value int) bool {
		collected[key] = value
		return true
	})

	if len(collected) != 3 {
		t.Errorf("Range collected %d items, want 3", len(collected))
	}

	for _, key := range []string{"a", "b", "c"} {
		if _, ok := collected[key]; !ok {
			t.Errorf("Range missing key %s", key)
		}
	}
}

// TestRangeEarlyStop tests that Range stops when function returns false.
func TestRangeEarlyStop(t *testing.T) {
	m := New[int, int]()

	tx := m.Begin()
	for i := 0; i < 10; i++ {
		_ = tx.Set(i, i*10)
	}
	_ = tx.Commit()

	count := 0
	m.Range(func(key int, value int) bool {
		count++
		return count < 3 // Stop after 3 items
	})

	if count != 3 {
		t.Errorf("Range should have stopped after 3 items, got %d", count)
	}
}

// TestTransactionBasicOps tests basic transaction operations.
func TestTransactionBasicOps(t *testing.T) {
	m := New[string, int]()

	// Set some initial data
	tx := m.Begin()
	_ = tx.Set("existing", 1)
	_ = tx.Commit()

	// Begin transaction
	tx = m.Begin()

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

// TestReadOnlyMapBehavior tests that the map is read-only outside transactions.
func TestReadOnlyMapBehavior(t *testing.T) {
	m := New[string, int]()

	// Set initial data via transaction
	tx := m.Begin()
	_ = tx.Set("key1", 10)
	_ = tx.Set("key2", 20)
	_ = tx.Commit()

	// Start tx1, then make changes via tx2, verify tx1 sees the new values
	tx1 := m.Begin()

	// Modify map via another transaction after tx1 started
	tx2 := m.Begin()
	_ = tx2.Set("key1", 100)
	_ = tx2.Set("key3", 30)
	_ = tx2.Commit()

	// tx1 should see the updated values since there's no snapshot copy
	value, ok := tx1.Get("key1")
	if !ok {
		t.Fatal("tx1 should see key1")
	}
	if value != 100 {
		t.Errorf("tx1 sees key1 = %d, want 100 (current value)", value)
	}

	// tx1 should see key3 which was added by tx2
	value, ok = tx1.Get("key3")
	if !ok {
		t.Fatal("tx1 should see key3")
	}
	if value != 30 {
		t.Errorf("tx1 sees key3 = %d, want 30", value)
	}
}

// TestTransactionDeleteOps tests deleting keys in a transaction.
func TestTransactionDeleteOps(t *testing.T) {
	m := New[string, string]()

	tx := m.Begin()
	_ = tx.Set("keep", "value1")
	_ = tx.Set("delete", "value2")
	_ = tx.Commit()

	tx = m.Begin()

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

// TestTransactionRollbackOps tests rolling back a transaction.
func TestTransactionRollbackOps(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("original", 1)
	_ = tx.Commit()

	tx = m.Begin()

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

// TestTransactionClosedErr tests that operations fail on closed transactions.
func TestTransactionClosedErr(t *testing.T) {
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

// TestPreCommitHookWithNewSignature tests pre-commit hooks with the new signature.
func TestPreCommitHookWithNewSignature(t *testing.T) {
	m := New[string, int]()

	hookCalled := false
	var capturedChanges map[string]int

	m.AddPreCommitHook(func(changes map[string]int) error {
		hookCalled = true
		capturedChanges = make(map[string]int)
		for k, v := range changes {
			capturedChanges[k] = v
		}
		return nil
	})

	tx := m.Begin()
	_ = tx.Set("testkey", 42)
	err := tx.Commit()
	if err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}

	if !hookCalled {
		t.Error("Pre-commit hook was not called")
	}
	if capturedChanges["testkey"] != 42 {
		t.Errorf("Hook received changes %v, want testkey=42", capturedChanges)
	}
}

// TestPreCommitHookModify tests that pre-commit hooks can modify changes.
func TestPreCommitHookModify(t *testing.T) {
	m := New[string, int]()

	// Hook that doubles all values
	m.AddPreCommitHook(func(changes map[string]int) error {
		for k, v := range changes {
			changes[k] = v * 2
		}
		return nil
	})

	tx := m.Begin()
	_ = tx.Set("key", 50)
	_ = tx.Commit()

	value, ok := m.Get("key")
	if !ok {
		t.Fatal("Get() returned false")
	}
	if value != 100 {
		t.Errorf("Get() = %d, want 100 (modified by hook)", value)
	}
}

// TestPreCommitHookAbort tests that pre-commit hook errors abort the operation.
func TestPreCommitHookAbort(t *testing.T) {
	m := New[string, int]()

	expectedErr := errors.New("validation failed")

	m.AddPreCommitHook(func(changes map[string]int) error {
		for _, v := range changes {
			if v < 0 {
				return expectedErr
			}
		}
		return nil
	})

	// Positive value should succeed
	tx := m.Begin()
	_ = tx.Set("positive", 10)
	err := tx.Commit()
	if err != nil {
		t.Fatalf("Commit() with positive value returned error: %v", err)
	}

	// Negative value should fail
	tx = m.Begin()
	_ = tx.Set("negative", -5)
	err = tx.Commit()
	if err != expectedErr {
		t.Errorf("Commit() with negative value: got %v, want %v", err, expectedErr)
	}

	// Verify the negative value was not set
	_, ok := m.Get("negative")
	if ok {
		t.Error("Negative value should not have been set")
	}
}

// TestPostCommitHookNotifications tests post-commit hooks for notifications.
func TestPostCommitHookNotifications(t *testing.T) {
	m := New[string, int]()

	var receivedChanges map[string]int
	hookCalled := false

	m.AddPostCommitHook(func(changes map[string]int) {
		hookCalled = true
		receivedChanges = changes
	})

	tx := m.Begin()
	_ = tx.Set("first", 1)
	_ = tx.Set("second", 2)
	_ = tx.Commit()

	if !hookCalled {
		t.Error("Post-commit hook was not called")
	}
	if len(receivedChanges) != 2 {
		t.Errorf("Post-commit hook received %d changes, want 2", len(receivedChanges))
	}
	if receivedChanges["first"] != 1 || receivedChanges["second"] != 2 {
		t.Errorf("Post-commit hook received wrong changes: %v", receivedChanges)
	}
}

// TestPostCommitHookWithDeletes tests post-commit hooks with deletes (zero values).
func TestPostCommitHookWithDeletes(t *testing.T) {
	m := New[string, *int]()

	// Set up initial data with pointer values
	val1 := 1
	val2 := 2
	tx := m.Begin()
	_ = tx.Set("key1", &val1)
	_ = tx.Set("key2", &val2)
	_ = tx.Commit()

	var receivedChanges map[string]*int

	m.AddPostCommitHook(func(changes map[string]*int) {
		receivedChanges = changes
	})

	val3 := 3
	tx = m.Begin()
	_ = tx.Set("key3", &val3)
	_ = tx.Delete("key1")
	_ = tx.Commit()

	if receivedChanges["key3"] == nil || *receivedChanges["key3"] != 3 {
		t.Errorf("Post-commit hook missing key3 write")
	}
	// Deleted key should have nil (zero value for pointer)
	if _, exists := receivedChanges["key1"]; !exists {
		t.Error("Post-commit hook should include deleted key1")
	}
	if receivedChanges["key1"] != nil {
		t.Error("Deleted key1 should have nil value in changes")
	}
}

// TestNoChangeIfValueIdentical tests that identical values are not tracked.
func TestNoChangeIfValueIdentical(t *testing.T) {
	type Item struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	m := New[string, *Item]()

	item := &Item{Name: "test", Value: 42}
	tx := m.Begin()
	_ = tx.Set("key", item)
	_ = tx.Commit()

	var changeCount int
	m.AddPostCommitHook(func(changes map[string]*Item) {
		changeCount = len(changes)
	})

	// Set identical value - should not trigger change
	identicalItem := &Item{Name: "test", Value: 42}
	tx = m.Begin()
	_ = tx.Set("key", identicalItem)
	_ = tx.Commit()

	if changeCount != 0 {
		t.Errorf("Expected 0 changes for identical value, got %d", changeCount)
	}

	// Set different value - should trigger change
	differentItem := &Item{Name: "test", Value: 100}
	tx = m.Begin()
	_ = tx.Set("key", differentItem)
	_ = tx.Commit()

	if changeCount != 1 {
		t.Errorf("Expected 1 change for different value, got %d", changeCount)
	}
}

// TestTransactionHooksCalled tests that hooks are called during transaction commit.
func TestTransactionHooksCalled(t *testing.T) {
	m := New[string, int]()

	var preCommitCalled bool
	var postCommitCalled bool

	m.AddPreCommitHook(func(changes map[string]int) error {
		preCommitCalled = true
		return nil
	})
	m.AddPostCommitHook(func(changes map[string]int) {
		postCommitCalled = true
	})

	// Set up initial data
	tx := m.Begin()
	_ = tx.Set("existing", 1)
	_ = tx.Commit()

	// Reset flags
	preCommitCalled = false
	postCommitCalled = false

	tx = m.Begin()
	_ = tx.Set("new1", 10)
	_ = tx.Set("new2", 20)
	_ = tx.Delete("existing")

	// Hooks should not be called yet
	if preCommitCalled || postCommitCalled {
		t.Error("Hooks should not be called before commit")
	}

	err := tx.Commit()
	if err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}

	// Verify hooks were called
	if !preCommitCalled {
		t.Error("Pre-commit hook was not called")
	}
	if !postCommitCalled {
		t.Error("Post-commit hook was not called")
	}
}

// TestMultipleHooksOrder tests that multiple hooks are called in order.
func TestMultipleHooksOrder(t *testing.T) {
	m := New[string, int]()

	var order []int

	m.AddPreCommitHook(func(changes map[string]int) error {
		order = append(order, 1)
		return nil
	})
	m.AddPreCommitHook(func(changes map[string]int) error {
		order = append(order, 2)
		return nil
	})
	m.AddPostCommitHook(func(changes map[string]int) {
		order = append(order, 3)
	})
	m.AddPostCommitHook(func(changes map[string]int) {
		order = append(order, 4)
	})

	tx := m.Begin()
	_ = tx.Set("key", 1)
	_ = tx.Commit()

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

// TestTransactionLenMethod tests the Len method on transactions.
func TestTransactionLenMethod(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("a", 1)
	_ = tx.Set("b", 2)
	_ = tx.Commit()

	tx = m.Begin()

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

// TestTransactionRange tests Range on transaction.
func TestTransactionRange(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("a", 1)
	_ = tx.Set("b", 2)
	_ = tx.Commit()

	tx = m.Begin()
	_ = tx.Set("c", 3)
	_ = tx.Delete("a")

	collected := make(map[string]int)
	tx.Range(func(key string, value int) bool {
		collected[key] = value
		return true
	})

	// Should see b and c, not a
	if len(collected) != 2 {
		t.Errorf("Transaction Range collected %d items, want 2", len(collected))
	}
	if _, ok := collected["a"]; ok {
		t.Error("Transaction Range should not include deleted key 'a'")
	}
	if collected["b"] != 2 {
		t.Errorf("Transaction Range missing or wrong value for 'b'")
	}
	if collected["c"] != 3 {
		t.Errorf("Transaction Range missing or wrong value for 'c'")
	}
}

// TestTransactionPendingChangesMethod tests the PendingChanges method.
func TestTransactionPendingChangesMethod(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("existing", 1)
	_ = tx.Commit()

	tx = m.Begin()

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

// TestConcurrentReadOps tests concurrent read operations.
func TestConcurrentReadOps(t *testing.T) {
	m := New[int, int]()

	// Set up initial data via transaction
	tx := m.Begin()
	for i := 0; i < 100; i++ {
		_ = tx.Set(i, i*10)
	}
	_ = tx.Commit()

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

// TestConcurrentTransactionOps tests multiple concurrent transactions.
func TestConcurrentTransactionOps(t *testing.T) {
	m := New[string, int]()

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

// TestGenericTypeCombinations tests the map with various generic type combinations.
func TestGenericTypeCombinations(t *testing.T) {
	t.Run("StringToStruct", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}

		m := New[string, Person]()
		tx := m.Begin()
		_ = tx.Set("alice", Person{Name: "Alice", Age: 30})
		_ = tx.Commit()

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
		tx := m.Begin()
		_ = tx.Set(1, []string{"a", "b", "c"})
		_ = tx.Commit()

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
		tx := m.Begin()
		_ = tx.Set(Key{1, 2}, map[string]int{"value": 42})
		_ = tx.Commit()

		value, ok := m.Get(Key{1, 2})
		if !ok {
			t.Fatal("Get() returned false")
		}
		if value["value"] != 42 {
			t.Errorf("Got %v, want map[value:42]", value)
		}
	})
}

// TestTransactionOverwriteOps tests overwriting values in a transaction.
func TestTransactionOverwriteOps(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("key", 1)
	_ = tx.Commit()

	tx = m.Begin()

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

// TestTransactionDeleteThenSetOps tests deleting then re-setting a key.
func TestTransactionDeleteThenSetOps(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("key", 1)
	_ = tx.Commit()

	tx = m.Begin()

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

// TestTransactionSetThenDeleteOps tests setting then deleting a key.
func TestTransactionSetThenDeleteOps(t *testing.T) {
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

// TestEmptyTransactionCommit tests committing an empty transaction.
func TestEmptyTransactionCommit(t *testing.T) {
	m := New[string, int]()

	tx := m.Begin()
	_ = tx.Set("key", 1)
	_ = tx.Commit()

	tx = m.Begin()

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

// TestDeleteNonExistentKeyOp tests deleting a key that doesn't exist.
func TestDeleteNonExistentKeyOp(t *testing.T) {
	m := New[string, int]()

	// Transaction delete of non-existent key
	tx := m.Begin()
	err := tx.Delete("nonexistent")
	if err != nil {
		t.Errorf("Transaction Delete(nonexistent) returned error: %v", err)
	}

	// Should have no pending changes since key didn't exist
	if tx.PendingChanges() != 0 {
		t.Errorf("PendingChanges() = %d, want 0 for deleting non-existent key", tx.PendingChanges())
	}

	err = tx.Commit()
	if err != nil {
		t.Errorf("Commit after deleting nonexistent key returned error: %v", err)
	}
}

// BenchmarkTransactionSetOp benchmarks the Set operation via transaction.
func BenchmarkTransactionSetOp(b *testing.B) {
	m := New[int, int]()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := m.Begin()
		_ = tx.Set(i, i)
		_ = tx.Commit()
	}
}

// BenchmarkGetOp benchmarks the Get operation.
func BenchmarkGetOp(b *testing.B) {
	m := New[int, int]()
	tx := m.Begin()
	for i := 0; i < 1000; i++ {
		_ = tx.Set(i, i)
	}
	_ = tx.Commit()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Get(i % 1000)
	}
}

// BenchmarkBeginNoSnapshot benchmarks Begin which no longer copies the map.
func BenchmarkBeginNoSnapshot(b *testing.B) {
	m := New[int, int]()
	// Pre-populate with some data
	tx := m.Begin()
	for i := 0; i < 10000; i++ {
		_ = tx.Set(i, i)
	}
	_ = tx.Commit()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Begin()
	}
}

// BenchmarkTransactionWithHooksOp benchmarks transactions with hooks.
func BenchmarkTransactionWithHooksOp(b *testing.B) {
	m := New[int, int]()
	m.AddPreCommitHook(func(changes map[int]int) error { return nil })
	m.AddPostCommitHook(func(changes map[int]int) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := m.Begin()
		_ = tx.Set(i, i)
		_ = tx.Commit()
	}
}
