package ids

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

func New(prefix string) string {
	entropy := ulid.Monotonic(rand.Reader, 0)
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), entropy)
	if prefix == "" {
		return id.String()
	}
	return prefix + "_" + id.String()
}
