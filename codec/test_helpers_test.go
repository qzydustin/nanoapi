package codec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return body
}

func fixtureString(t *testing.T, name string) string {
	t.Helper()
	return strings.TrimSpace(string(fixtureBytes(t, name)))
}

type ssePair struct {
	Event string
	Data  string
}

func fixtureSSEPairs(t *testing.T, name string) []ssePair {
	t.Helper()

	lines := strings.Split(string(fixtureBytes(t, name)), "\n")
	var pairs []ssePair
	var current ssePair
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Event != "" || current.Data != "" {
				pairs = append(pairs, current)
				current = ssePair{}
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			current.Event = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			current.Data = strings.TrimPrefix(line, "data: ")
		}
	}
	if current.Event != "" || current.Data != "" {
		pairs = append(pairs, current)
	}
	return pairs
}
