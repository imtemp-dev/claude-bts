package metrics

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TokenCounts is the usage a set of model calls reported.
type TokenCounts struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheWrite int64 `json:"cache_write"`
	CacheRead  int64 `json:"cache_read"`
}

// Total is every token the calls processed or wrote.
func (c TokenCounts) Total() int64 { return c.Input + c.Output + c.CacheWrite + c.CacheRead }

func (c *TokenCounts) add(u transcriptUsage) {
	c.Input += u.Input
	c.Output += u.Output
	c.CacheWrite += u.CacheWrite
	c.CacheRead += u.CacheRead
}

// TranscriptTokens is what a recipe's Claude Code transcripts say it used:
// the main sessions and their subagents, within the recipe's time span.
// It carries no cost: prices change, and a table would need upkeep
// (trial problem #13, AGEN-66).
type TranscriptTokens struct {
	Main         TokenCounts `json:"main"`
	Subagents    TokenCounts `json:"subagents"`
	SubagentRuns int         `json:"subagent_runs"`
	Messages     int         `json:"messages"` // distinct assistant messages counted
	Sessions     []string    `json:"sessions"`
}

var slugRe = regexp.MustCompile(`[^A-Za-z0-9]`)

// TranscriptDir is where Claude Code keeps a project's transcripts:
// <config dir>/projects/<the root with every non-alphanumeric as "-">.
// The config dir is CLAUDE_CONFIG_DIR, else ~/.claude.
func TranscriptDir(root string) string {
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".claude")
	}
	return filepath.Join(base, "projects", slugRe.ReplaceAllString(root, "-"))
}

// SumTranscriptTokens adds up the usage in dir's transcripts: each listed
// session's main transcript and its subagents' (every session's, when
// none is listed), counting only messages within [from, to] (a zero bound
// is open).  Claude Code writes a line per content block and repeats the
// message's usage on each, so a message counts once, by its id.
func SumTranscriptTokens(dir string, sessions []string, from, to time.Time) (*TranscriptTokens, error) {
	if len(sessions) == 0 {
		mains, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, m := range mains {
			sessions = append(sessions, strings.TrimSuffix(filepath.Base(m), ".jsonl"))
		}
		sort.Strings(sessions)
	}
	res := &TranscriptTokens{}
	seen := map[string]bool{}
	for _, sid := range sessions {
		n, err := sumTranscriptFile(filepath.Join(dir, sid+".jsonl"), from, to, seen, &res.Main)
		if err != nil {
			return nil, err
		}
		subs, _ := filepath.Glob(filepath.Join(dir, sid, "subagents", "*.jsonl"))
		sub := 0
		for _, s := range subs {
			k, err := sumTranscriptFile(s, from, to, seen, &res.Subagents)
			if err != nil {
				return nil, err
			}
			if k > 0 {
				res.SubagentRuns++
			}
			sub += k
		}
		if n+sub > 0 {
			res.Sessions = append(res.Sessions, sid)
		}
		res.Messages += n + sub
	}
	return res, nil
}

type transcriptUsage struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
}

type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID    string           `json:"id"`
		Model string           `json:"model"`
		Usage *transcriptUsage `json:"usage"`
	} `json:"message"`
}

// TranscriptModel returns the model of the first assistant message in a
// session's main transcript, or "" when there is none.
func TranscriptModel(dir, session string) string {
	f, err := os.Open(filepath.Join(dir, session+".jsonl"))
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		raw, rerr := r.ReadBytes('\n')
		if len(raw) > 0 && bytes.Contains(raw, []byte(`"model"`)) {
			var l transcriptLine
			if json.Unmarshal(raw, &l) == nil && l.Type == "assistant" && l.Message.Model != "" {
				return l.Message.Model
			}
		}
		if rerr != nil {
			return ""
		}
	}
}

// sumTranscriptFile adds one transcript's messages to into and returns how
// many it counted.  A missing file counts nothing; a line that is not
// JSON is skipped, as a transcript can end mid-write.
func sumTranscriptFile(path string, from, to time.Time, seen map[string]bool, into *TokenCounts) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	counted := 0
	for {
		raw, rerr := r.ReadBytes('\n')
		if len(raw) > 0 && bytes.Contains(raw, []byte(`"usage"`)) {
			var l transcriptLine
			if json.Unmarshal(raw, &l) == nil && l.Type == "assistant" && l.Message.Usage != nil && l.Message.ID != "" && !seen[l.Message.ID] {
				ts, terr := time.Parse(time.RFC3339, l.Timestamp)
				if terr == nil && (from.IsZero() || !ts.Before(from)) && (to.IsZero() || !ts.After(to)) {
					seen[l.Message.ID] = true
					into.add(*l.Message.Usage)
					counted++
				}
			}
		}
		if rerr == io.EOF {
			return counted, nil
		}
		if rerr != nil {
			return counted, rerr
		}
	}
}
