package services

import "sync"

var repositoryOperationMu sync.Mutex

// LockRepositoryOperation serializes working-tree writes with sync and publish.
// The returned function must be deferred by the caller.
func LockRepositoryOperation() func() {
	repositoryOperationMu.Lock()
	return repositoryOperationMu.Unlock
}
