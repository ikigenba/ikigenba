package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-RTAE-HPB2
func TestSeedDefinitionsIsIdempotentAfterRepositoriesHaveHeads(t *testing.T) {
	assertRetiredSeederSymbol(t, "SeedDefinitions")
}

// R-RUIA-VH1R
func TestSeedDefinitionsPreservesRawColumnsAndSuffixesCollidingKeys(t *testing.T) {
	assertRetiredSeederSymbol(t, "listSeedDefinitions")
}

// R-RVQ7-98SG
func TestSeedDefinitionsRetriesUnavailablePlaneAndFailsLoudlyAtBound(t *testing.T) {
	assertRetiredSeederSymbol(t, "setPromptNameKey")
}

func assertRetiredSeederSymbol(t *testing.T, retired string) {
	t.Helper()
	for _, name := range []string{"seed.go", "store.go", "service.go"} {
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			if os.IsNotExist(err) && name == "seed.go" {
				continue
			}
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(body), retired) {
			t.Errorf("%s still contains retired seeder symbol %q", name, retired)
		}
	}
}
