package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// completionMarkers are the tagged markers that end a recipe stage.
var completionMarkers = []string{"<bts>DONE</bts>", "<bts>IMPLEMENT DONE</bts>", "<bts>FIX DONE</bts>"}

func hasCompletionMarker(text string) bool {
	for _, m := range completionMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// transcriptTailBytes bounds how much of the transcript the stop hook
// reads: the turn's last message is at the end of the file.
const transcriptTailBytes = 4 << 20

// markerTextFromTranscript returns the turn's last assistant message from
// the transcript, code stripped, when the stop payload's text lacks a
// completion marker the message has.  On true-shark the marker stood near
// the start of a 5,548-character message and the hook saw none, most
// likely because the payload carried only the message's tail (trial
// problem #85, AGEN-69).  The transcript's message counts only when it
// contains the payload's text, so a previous turn's message, read before
// this one reached the file, never completes anything.  "" when there is
// nothing to use.
func markerTextFromTranscript(path, payload string) string {
	payload = strings.TrimSpace(payload)
	if path == "" || payload == "" {
		return ""
	}
	full := lastAssistantText(path)
	if full == "" {
		return ""
	}
	probe := payload
	if len(probe) > 200 {
		probe = probe[len(probe)-200:]
	}
	if !strings.Contains(full, probe) {
		return ""
	}
	text := stripCode(full)
	if !hasCompletionMarker(text) {
		return ""
	}
	return text
}

// lastAssistantText is the text of the transcript's last main-chain
// assistant message, every text block of it, read from the file's tail.
func lastAssistantText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	off := info.Size() - transcriptTailBytes
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil || !(bytes.Contains(data, []byte("<bts>")) || bytes.Contains(data, []byte(`<bts>`))) {
		return "" // no marker anywhere in the tail: nothing to parse
	}
	if off > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	var lastID string
	var texts []string
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
		}
		var l struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				ID      string `json:"id"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.IsSidechain {
			continue
		}
		if l.Message.ID != lastID {
			lastID, texts = l.Message.ID, nil
		}
		for _, c := range l.Message.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
	}
	return strings.Join(texts, "\n")
}
