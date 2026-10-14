//go:build !unix

package history

import "os"

// fcntlTryLock is a no-op where fcntl locks are unavailable; the .LOCK file
// still applies.
func fcntlTryLock(*os.File) (bool, error) { return false, nil }
