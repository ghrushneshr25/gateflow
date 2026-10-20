package appnexus

import "github.com/ghrushneshr25/nexus"

// ResetForTest clears the process-local nexus registry.
// Call at the start of each unit test that registers mocks.
func ResetForTest() {
	nexus.Reset()
}
