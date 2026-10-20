// Package ids generates timestamp-sortable unique identifiers using ULID.
//
// Format: [prefix_]ULID where ULID encodes UTC milliseconds + monotonic entropy.
// Example: ord_01J8Z3Y5K6N7P8Q9R0S1T2U3V4
package ids

import (
	"crypto/rand"
	"io"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropyMu sync.Mutex
	entropy   io.Reader
)

func init() {
	entropy = ulid.Monotonic(rand.Reader, 0)
}

// New returns a new ULID string, optionally with a business prefix.
// The ULID is lexicographically sortable by creation time (UTC).
func New(prefix string) string {
	entropyMu.Lock()
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), entropy)
	entropyMu.Unlock()
	if prefix == "" {
		return id.String()
	}
	return prefix + "_" + id.String()
}

// ULID returns a bare ULID with no prefix (same as New("")).
func ULID() string {
	return New("")
}
