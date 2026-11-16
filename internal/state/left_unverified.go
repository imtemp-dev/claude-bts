package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// A verified document left unverified on purpose.  Rule 3 blocks a turn
// end while a verified document has changed since its last verification,
// and its message offered a second exit, "tell the user the doc is left
// unverified", which the hook cannot see: on warm-owl's r-003 the session
// told the user final.md stayed unverified until a test agent rewrote one
// section, and the hook blocked again with the same text (trial problem
// #136).
//
// `bts recipe leave-unverified` records the document, why it stays
// unverified, and a fingerprint of what made it stale: its own text and
// the documents its latest round read.  The blind-stop check lets the turn
// end while the fingerprint holds; any further change to the document or
// to one its review read re-arms rule 3, and so does a logged round for
// it.  No completion gate reads this record: DONE still needs the round.

// LeftUnverified is one document's record.
type LeftUnverified struct {
	Doc         string `json:"doc"`
	Fingerprint string `json:"fingerprint"`
	Reason      string `json:"reason"`
	SessionID   string `json:"session_id,omitempty"`
	At          string `json:"at"`
}

func leftUnverifiedPath(root, recipeID string) string {
	return filepath.Join(LocalPath(root), "recipes", recipeID, "left-unverified.json")
}

func loadLeftUnverified(root, recipeID string) map[string]LeftUnverified {
	out := map[string]LeftUnverified{}
	data, err := os.ReadFile(leftUnverifiedPath(root, recipeID))
	if err != nil {
		return out
	}
	var list []LeftUnverified
	if json.Unmarshal(data, &list) != nil {
		return out
	}
	for _, l := range list {
		out[l.Doc] = l
	}
	return out
}

func saveLeftUnverified(root, recipeID string, recs map[string]LeftUnverified) error {
	path := leftUnverifiedPath(root, recipeID)
	if len(recs) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	list := make([]LeftUnverified, 0, len(recs))
	for _, l := range recs {
		list = append(list, l)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Doc < list[j].Doc })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// staleFingerprint hashes what makes doc stale against its last
// verification: its own text, where its last full pass read it, and the
// current text of every document its latest round read.
func staleFingerprint(root, recipeID, doc string) (string, error) {
	entries, err := ReadVerifyLog(root, recipeID)
	if err != nil {
		return "", err
	}
	at := filepath.Join(RecipeDir(root, recipeID), doc)
	if p := LastFullPassPaths(entries)[doc]; p != "" {
		at = filepath.Join(root, p)
	}
	h := sha256.New()
	own, ok, err := FileContentHash(at)
	if err != nil {
		return "", err
	}
	if !ok {
		own = "absent"
	}
	fmt.Fprintf(h, "%s=%s\n", doc, own)
	if scoped := VerifyEntriesForDoc(entries, doc); len(scoped) > 0 {
		var names []string
		for name := range scoped[len(scoped)-1].CitedHashes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			cur, ok, err := FileContentHash(filepath.Join(RecipeDir(root, recipeID), name))
			if err != nil {
				return "", err
			}
			if !ok {
				cur = "absent"
			}
			fmt.Fprintf(h, "%s=%s\n", name, cur)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RecordLeftUnverified records doc as left unverified, for reason, as it
// is now.  doc is a basename DirtyVerifiedDocs reports.
func RecordLeftUnverified(root, recipeID, doc, reason, sessionID string) (LeftUnverified, error) {
	fp, err := staleFingerprint(root, recipeID, doc)
	if err != nil {
		return LeftUnverified{}, err
	}
	rec := LeftUnverified{Doc: doc, Fingerprint: fp, Reason: reason, SessionID: sessionID,
		At: time.Now().UTC().Format(time.RFC3339)}
	recs := loadLeftUnverified(root, recipeID)
	recs[doc] = rec
	return rec, saveLeftUnverified(root, recipeID, recs)
}

// HeldUnverified splits dirty documents into those a record still holds
// (left unverified, and nothing changed since) and the rest.  A record
// whose fingerprint no longer matches does not hold.
func HeldUnverified(root, recipeID string, dirty []string) (held []LeftUnverified, open []string) {
	recs := loadLeftUnverified(root, recipeID)
	for _, d := range dirty {
		rec, ok := recs[d]
		if ok {
			if fp, err := staleFingerprint(root, recipeID, d); err == nil && fp == rec.Fingerprint {
				held = append(held, rec)
				continue
			}
		}
		open = append(open, d)
	}
	return held, open
}

// ClearLeftUnverified drops doc's record: a round logged for it ends it.
func ClearLeftUnverified(root, recipeID, doc string) {
	recs := loadLeftUnverified(root, recipeID)
	if _, ok := recs[doc]; !ok {
		return
	}
	delete(recs, doc)
	_ = saveLeftUnverified(root, recipeID, recs)
}
