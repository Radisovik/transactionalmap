# transactionalmap

A Go library for a generic map that supports transactions with pre-commit and post-commit hooks. The map is read-only outside of transactions, avoiding the need for snapshot copies.

## Features

- **Generics Support**: Works with any comparable key type and any value type
- **Read-Only Map**: The map can only be modified through transactions
- **No Snapshot Copies**: Since the map is read-only, no expensive snapshot copies are needed
- **Pre-commit Hooks**: Modify/polish items before they are committed (receives all changes)
- **Post-commit Hooks**: Receive notifications of changes (useful for emitting JSON merge patches)
- **Smart Change Detection**: Uses JSON merge patch (RFC 7396) to detect if values actually changed
- **Thread-Safe**: All operations are protected by mutexes for concurrent access
- **Atomic Commits**: If any pre-commit hook fails, the entire transaction is aborted

## Installation

```bash
go get github.com/Radisovik/transactionalmap
```

## Usage

### Basic Operations

```go
package main

import (
    "fmt"
    tm "github.com/Radisovik/transactionalmap"
)

func main() {
    // Create a new map with string keys and pointer values
    m := tm.New[string, *Item]()

    // All modifications must be done through transactions
    tx := m.Begin()
    tx.Set("foo", &Item{Value: 42})
    tx.Set("bar", &Item{Value: 100})
    tx.Commit()

    // Read values directly from the map
    value, ok := m.Get("foo")
    if ok {
        fmt.Println("foo =", value.Value) // Output: foo = 42
    }

    // Get length
    fmt.Println("Length:", m.Len()) // Output: Length: 2

    // Iterate over all items
    m.Range(func(key string, value *Item) bool {
        fmt.Printf("%s: %d\n", key, value.Value)
        return true // continue iteration
    })
}
```

### Transactions

```go
package main

import (
    "fmt"
    tm "github.com/Radisovik/transactionalmap"
)

func main() {
    m := tm.New[string, *Account]()

    // Initialize with a transaction
    tx := m.Begin()
    tx.Set("balance", &Account{Amount: 100})
    tx.Commit()

    // Begin a new transaction
    tx = m.Begin()

    // Read current value
    balance, _ := tx.Get("balance")
    fmt.Println("Initial balance:", balance.Amount) // Output: Initial balance: 100

    // Stage changes (not visible to the main map until commit)
    tx.Set("balance", &Account{Amount: balance.Amount + 50})
    tx.Set("pending", &Account{Amount: 50})

    // Changes are not visible in the main map until commit
    mainBalance, _ := m.Get("balance")
    fmt.Println("Main map balance:", mainBalance.Amount) // Output: Main map balance: 100

    // Commit the transaction
    err := tx.Commit()
    if err != nil {
        fmt.Println("Commit failed:", err)
        return
    }

    // Now changes are visible in the main map
    finalBalance, _ := m.Get("balance")
    fmt.Println("Final balance:", finalBalance.Amount) // Output: Final balance: 150
}
```

### Rollback

```go
tx := m.Begin()
tx.Set("key", &Item{Value: 100})

// Discard all changes
tx.Rollback()

// The main map is unchanged
_, ok := m.Get("key")
fmt.Println("Key exists:", ok) // Output: Key exists: false
```

### Pre-commit Hooks (Modify/Polish Items)

Pre-commit hooks receive ALL pending changes and can modify them before they are committed. This is useful for polishing or transforming data.

```go
m := tm.New[string, *Item]()

// Add a hook that doubles all values
m.AddPreCommitHook(func(changes map[string]*Item) error {
    for key, item := range changes {
        if item != nil { // nil means deletion
            item.Value *= 2
        }
    }
    return nil
})

// Or add validation
m.AddPreCommitHook(func(changes map[string]*Item) error {
    for key, item := range changes {
        if item != nil && item.Value < 0 {
            return fmt.Errorf("value for %s must be non-negative", key)
        }
    }
    return nil
})

tx := m.Begin()
tx.Set("a", &Item{Value: 10})
tx.Set("b", &Item{Value: 20})
tx.Commit()

// Values are doubled by the hook
a, _ := m.Get("a")
fmt.Println("a:", a.Value) // Output: a: 20
```

### Post-commit Hooks (Notifications / JSON Merge Patch)

Post-commit hooks receive all changes that were applied. Deleted keys have a nil (zero) value. This is useful for emitting notifications or JSON merge patches.

```go
m := tm.New[string, *Item]()

// Add a notification hook
m.AddPostCommitHook(func(changes map[string]*Item) {
    for key, value := range changes {
        if value == nil {
            fmt.Printf("Deleted: %s\n", key)
        } else {
            fmt.Printf("Set: %s = %v\n", key, value)
        }
    }
})

tx := m.Begin()
tx.Set("new", &Item{Value: 42})
tx.Delete("old")
tx.Commit()
// Output:
// Set: new = &{42}
// Deleted: old
```

### Smart Change Detection

The library uses JSON merge patch (RFC 7396) to detect if values actually changed. If you set a value that's identical to the current value, no change is recorded.

```go
m := tm.New[string, *Item]()

tx := m.Begin()
tx.Set("key", &Item{Name: "test", Value: 42})
tx.Commit()

var changeCount int
m.AddPostCommitHook(func(changes map[string]*Item) {
    changeCount = len(changes)
})

// Set identical value - no change recorded
tx = m.Begin()
tx.Set("key", &Item{Name: "test", Value: 42})
tx.Commit()
fmt.Println("Changes:", changeCount) // Output: Changes: 0

// Set different value - change recorded
tx = m.Begin()
tx.Set("key", &Item{Name: "test", Value: 100})
tx.Commit()
fmt.Println("Changes:", changeCount) // Output: Changes: 1
```

## API Reference

### Map[K, V]

- `New[K comparable, V any]() *Map[K, V]` - Create a new transactional map
- `Get(key K) (V, bool)` - Get a value by key
- `Len() int` - Get the number of items
- `Range(fn func(key K, value V) bool)` - Iterate over all items
- `Begin() *Transaction[K, V]` - Start a new transaction
- `AddPreCommitHook(hook PreCommitHookFunc[K, V])` - Add a pre-commit hook
- `AddPostCommitHook(hook PostCommitHookFunc[K, V])` - Add a post-commit hook

### Transaction[K, V]

- `Get(key K) (V, bool)` - Get a value from the transaction's view
- `Set(key K, value V) error` - Stage a value to be set on commit
- `Delete(key K) error` - Stage a key to be deleted on commit
- `Commit() error` - Apply all staged changes to the parent map
- `Rollback() error` - Discard all staged changes
- `Len() int` - Get the number of items visible in this transaction
- `Range(fn func(key K, value V) bool)` - Iterate over items in transaction view
- `PendingChanges() int` - Get the number of staged changes
- `IsCommitted() bool` - Check if the transaction has been committed
- `IsRolledBack() bool` - Check if the transaction has been rolled back
- `IsClosed() bool` - Check if the transaction is closed

### Hook Types

- `PreCommitHookFunc[K, V] func(changes map[K]V) error` - Hook for modifying changes before commit
- `PostCommitHookFunc[K, V] func(changes map[K]V)` - Hook for notifications after commit

### Change Representation

In the changes map passed to hooks:
- **Writes**: Key maps to the new value
- **Deletes**: Key maps to the zero value (nil for pointers)

## Thread Safety

All operations on the map and transactions are protected by mutexes. It's safe to:

- Access the map from multiple goroutines
- Have multiple concurrent transactions
- Read from a transaction while other transactions are committing

Note: Since the map is read-only outside transactions, there's no snapshot isolation. A transaction sees the current state of the map, including changes committed by other transactions.

## Dependencies

- [github.com/evanphx/json-patch/v5](https://github.com/evanphx/json-patch) - For RFC 7396 JSON Merge Patch

## License

MIT
