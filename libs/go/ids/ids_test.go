package ids

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func TestNewPrefixedULID(t *testing.T) {
	id := New("ord")
	if !strings.HasPrefix(id, "ord_") {
		t.Fatalf("%s", id)
	}
	raw := strings.TrimPrefix(id, "ord_")
	parsed, err := ulid.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	ts := ulid.Time(parsed.Time())
	if time.Since(ts) > time.Minute || ts.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("unexpected timestamp %v", ts)
	}
}

func TestNewSortableByTime(t *testing.T) {
	a := New("")
	time.Sleep(2 * time.Millisecond)
	b := New("")
	if a >= b {
		t.Fatalf("expected a < b: %s vs %s", a, b)
	}
}

func TestConcurrentUnique(t *testing.T) {
	const n = 200
	seen := make(map[string]struct{}, n)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := New("x")
			mu.Lock()
			if _, ok := seen[id]; ok {
				t.Errorf("duplicate %s", id)
			}
			seen[id] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("got %d unique", len(seen))
	}
}
