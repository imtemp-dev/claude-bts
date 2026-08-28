package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ToolTraceEntry is one record in tool-trace.jsonl capturing either the
// intent (phase=pre) or the result (phase=post) of a tool invocation.
type ToolTraceEntry struct {
	Time     string `json:"t"`
	Phase    string `json:"phase"`
	ToolName string `json:"tool"`
	File     string `json:"file,omitempty"`
	Command  string `json:"cmd,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

// ToolTraceMaxLines caps the trace file length. When append crosses this
// threshold the file is rewritten with only the tail kept.
const ToolTraceMaxLines = 50

// ToolTracePath returns the path to the tool-trace file.
func ToolTracePath(root string) string {
	return filepath.Join(LocalPath(root), "tool-trace.jsonl")
}

// AppendToolTrace appends one entry and truncates the file to the tail
// when it grows past ToolTraceMaxLines. Failure is silent-friendly for
// callers — errors are returned but hook callers are expected to ignore.
func AppendToolTrace(root string, e *ToolTraceEntry) error {
	if e == nil {
		return nil
	}
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}
	path := ToolTracePath(root)
	if err := AppendJSONL(path, e); err != nil {
		return err
	}
	return truncateToolTrace(path, ToolTraceMaxLines)
}

// TailToolTrace returns the last n entries from tool-trace.jsonl.
// Returns nil, nil if the file doesn't exist or is empty.
func TailToolTrace(root string, n int) ([]*ToolTraceEntry, error) {
	if n <= 0 {
		return nil, nil
	}
	path := ToolTracePath(root)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []*ToolTraceEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e ToolTraceEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		all = append(all, &e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

// truncateToolTrace rewrites the file with only the last max lines if the
// current file exceeds 2*max lines. The 2x threshold avoids rewriting on
// every single append once we cross the limit.
func truncateToolTrace(path string, max int) error {
	// Quick line count — avoid loading the whole file when small
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines, err := readAllLines(f)
	f.Close()
	if err != nil {
		return err
	}
	if len(lines) <= max*2 {
		return nil
	}
	tail := lines[len(lines)-max:]
	return writeLinesAtomic(path, tail)
}

func readAllLines(f *os.File) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func writeLinesAtomic(path string, lines []string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".jig-tooltrace-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	w := bufio.NewWriter(tmp)
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	tmp.Close()
	return os.Rename(tmpName, path)
}
