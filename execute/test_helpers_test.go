package execute

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

func fixtureLines(t *testing.T, name string) []string {
	t.Helper()

	raw := strings.Split(string(fixtureBytes(t, name)), "\n")
	var lines []string
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

type ssePair struct {
	event string
	data  string
}

func fixtureSSEPairs(t *testing.T, name string) []ssePair {
	t.Helper()

	lines := strings.Split(string(fixtureBytes(t, name)), "\n")
	var pairs []ssePair
	var current ssePair
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.event != "" || current.data != "" {
				pairs = append(pairs, current)
				current = ssePair{}
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			current.event = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			current.data = strings.TrimPrefix(line, "data: ")
		}
	}
	if current.event != "" || current.data != "" {
		pairs = append(pairs, current)
	}
	return pairs
}
