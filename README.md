# transactionalmap

A Go library for a generic map that supports transactions with snapshot isolation, pre-commit and post-commit hooks.

## Features

- **Generics Support**: Works with any comparable key type and any value type
- **Transaction Support**: Begin, commit, and rollback transactions with snapshot isolation
- **Pre-commit Hooks**: Execute validation logic before changes are committed
- **Post-commit Hooks**: Execute side effects after changes are committed
- **Pre/Post Delete Hooks**: Separate hooks for delete operations
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
    // Create a new map with string keys and int values
    m := tm.New[string, int]()

    // Set values
    m.Set("foo", 42)
    m.Set("bar", 100)

    // Get values
    value, ok := m.Get("foo")
    if ok {
        fmt.Println("foo =", value) // Output: foo = 42
    }

    // Delete values
    m.Delete("bar")

    // Get length
    fmt.Println("Length:", m.Len()) // Output: Length: 1

    // Get all keys
    keys := m.Keys()
    fmt.Println("Keys:", keys) // Output: Keys: [foo]

    // Clear all values
    m.Clear()
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
    m := tm.New[string, int]()
    m.Set("balance", 100)

    // Begin a transaction
    tx := m.Begin()

    // All reads see a snapshot from when the transaction began
    balance, _ := tx.Get("balance")
    fmt.Println("Initial balance:", balance) // Output: Initial balance: 100

    // Stage changes (not visible to other transactions or the main map)
    tx.Set("balance", balance + 50)
    tx.Set("pending", 50)

    // Verify changes within transaction
    newBalance, _ := tx.Get("balance")
    fmt.Println("New balance:", newBalance) // Output: New balance: 150

    // Changes are not visible in the main map until commit
    mainBalance, _ := m.Get("balance")
    fmt.Println("Main map balance:", mainBalance) // Output: Main map balance: 100

    // Commit the transaction
    err := tx.Commit()
    if err != nil {
        fmt.Println("Commit failed:", err)
        return
    }

    // Now changes are visible in the main map
    finalBalance, _ := m.Get("balance")
    fmt.Println("Final balance:", finalBalance) // Output: Final balance: 150
}
```

### Rollback

```go
tx := m.Begin()
tx.Set("key", 100)

// Discard all changes
tx.Rollback()

// The main map is unchanged
_, ok := m.Get("key")
fmt.Println("Key exists:", ok) // Output: Key exists: false
```

### Pre-commit Hooks

Pre-commit hooks are called before each change is applied. If any hook returns an error, the entire transaction is aborted.

```go
m := tm.New[string, int]()

// Add a validation hook
m.AddPreCommitHook(func(key string, value int) error {
    if value < 0 {
        return fmt.Errorf("value must be non-negative")
    }
    return nil
})

// This will fail
err := m.Set("balance", -100)
if err != nil {
    fmt.Println("Set failed:", err) // Output: Set failed: value must be non-negative
}

// In transactions, hooks are called during Commit()
tx := m.Begin()
tx.Set("a", 10)
tx.Set("b", -5) // Invalid value

err = tx.Commit()
if err != nil {
    fmt.Println("Commit failed:", err) // Output: Commit failed: value must be non-negative
}

// Neither change was applied because the transaction is atomic
_, ok := m.Get("a")
fmt.Println("a exists:", ok) // Output: a exists: false
```

### Post-commit Hooks

Post-commit hooks are called after changes have been applied. They're useful for side effects like logging or notifications.

```go
m := tm.New[string, int]()

// Add a logging hook
m.AddPostCommitHook(func(key string, value int) error {
    fmt.Printf("Changed %s to %d\n", key, value)
    return nil
})

m.Set("count", 42) // Output: Changed count to 42
```

### Delete Hooks

```go
m := tm.New[string, string]()
m.Set("protected", "important data")

// Prevent deletion of protected keys
m.AddPreDeleteHook(func(key string) error {
    if key == "protected" {
        return fmt.Errorf("cannot delete protected key")
    }
    return nil
})

err := m.Delete("protected")
if err != nil {
    fmt.Println("Delete failed:", err) // Output: Delete failed: cannot delete protected key
}

// Add a notification for deletions
m.AddPostDeleteHook(func(key string) error {
    fmt.Printf("Key %s was deleted\n", key)
    return nil
})
```

### Using with Custom Types

```go
// Custom struct as value
type User struct {
    Name  string
    Email string
    Age   int
}

users := tm.New[int, User]()
users.Set(1, User{Name: "Alice", Email: "alice@example.com", Age: 30})

// Custom struct as key (must be comparable)
type Point struct {
    X, Y int
}

points := tm.New[Point, string]()
points.Set(Point{1, 2}, "origin")
```

## API Reference

### Map[K, V]

- `New[K comparable, V any]() *Map[K, V]` - Create a new transactional map
- `Get(key K) (V, bool)` - Get a value by key
- `Set(key K, value V) error` - Set a key-value pair
- `Delete(key K) error` - Delete a key
- `Len() int` - Get the number of items
- `Keys() []K` - Get all keys
- `Values() []V` - Get all values
- `Clear()` - Remove all items
- `Begin() *Transaction[K, V]` - Start a new transaction
- `AddPreCommitHook(hook HookFunc[K, V])` - Add a pre-commit hook
- `AddPostCommitHook(hook HookFunc[K, V])` - Add a post-commit hook
- `AddPreDeleteHook(hook DeleteHookFunc[K])` - Add a pre-delete hook
- `AddPostDeleteHook(hook DeleteHookFunc[K])` - Add a post-delete hook

### Transaction[K, V]

- `Get(key K) (V, bool)` - Get a value from the transaction's view
- `Set(key K, value V) error` - Stage a value to be set on commit
- `Delete(key K) error` - Stage a key to be deleted on commit
- `Commit() error` - Apply all staged changes to the parent map
- `Rollback() error` - Discard all staged changes
- `Len() int` - Get the number of items visible in this transaction
- `PendingChanges() int` - Get the number of staged changes
- `IsCommitted() bool` - Check if the transaction has been committed
- `IsRolledBack() bool` - Check if the transaction has been rolled back
- `IsClosed() bool` - Check if the transaction is closed

### Hook Types

- `HookFunc[K comparable, V any] func(key K, value V) error` - Hook for set operations
- `DeleteHookFunc[K comparable] func(key K) error` - Hook for delete operations

## Thread Safety

All operations on the map and transactions are protected by mutexes. It's safe to:

- Access the map from multiple goroutines
- Have multiple concurrent transactions
- Read from a transaction while other transactions are committing

However, transactions provide snapshot isolation, not serializable isolation. If two transactions modify the same key, both can commit successfully, and the last commit wins.

## License

MIT
