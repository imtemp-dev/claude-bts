package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// exportDoc is one document of an ArtifactData out_dir export:
// <dir>/<collection>/<doc_id>.json.  The file holds either the document
// as the tool lists it ({"id", "data", "version"}) or its fields alone.
type exportDoc struct {
	ID   string
	Data map[string]interface{}
}

func readExport(dir, collection string) (map[string]exportDoc, error) {
	cdir := filepath.Join(dir, collection)
	entries, err := os.ReadDir(cdir)
	if os.IsNotExist(err) {
		return map[string]exportDoc{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]exportDoc{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(cdir, e.Name()))
		if err != nil {
			return nil, err
		}
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("%s/%s: %w", collection, e.Name(), err)
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if v, ok := m["id"].(string); ok && v != "" {
			id = v
		}
		data := m
		if d, ok := m["data"].(map[string]interface{}); ok {
			data = d
		}
		out[id] = exportDoc{ID: id, Data: data}
	}
	return out, nil
}

func str(m map[string]interface{}, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func num(m map[string]interface{}, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func strs(m map[string]interface{}, k string) []string {
	arr, _ := m[k].([]interface{})
	var out []string
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Import reads an ArtifactData export of the page's `rounds` and `notes`
// collections and records every round still waiting for the session
// (status submitted or picked), or only round `only` when it is set.  A
// round imported again keeps the dispositions and proposals already
// recorded for its notes.
func Import(sub Subject, dir string, only int) ([]*Round, error) {
	s, err := Load(sub)
	if err != nil {
		return nil, err
	}
	rounds, err := readExport(dir, "rounds")
	if err != nil {
		return nil, err
	}
	notes, err := readExport(dir, "notes")
	if err != nil {
		return nil, err
	}
	if len(rounds) == 0 {
		return nil, fmt.Errorf("no rounds in %s: export the page's rounds and notes with ArtifactData out_dir first", dir)
	}
	before, err := currentHashes(sub)
	if err != nil {
		return nil, err
	}
	var out []*Round
	for _, d := range rounds {
		n := num(d.Data, "round")
		if n <= 0 {
			continue
		}
		status := str(d.Data, "status")
		if only > 0 {
			if n != only {
				continue
			}
		} else if status != "submitted" && status != "picked" {
			continue
		}
		prev, _ := LoadRound(sub, n)
		if prev != nil && prev.Processed != "" && only == 0 {
			continue
		}
		kind := str(d.Data, "kind")
		if kind == "" {
			kind = KindReview
		}
		r := &Round{Recipe: sub.ID, Round: n, Kind: kind, Page: str(d.Data, "page"),
			Submitted: str(d.Data, "submitted"), Imported: now(), FilesBefore: before}
		if prev != nil {
			r.FilesBefore = prev.FilesBefore
			r.Proposals = prev.Proposals
			r.Revisions = prev.Revisions
			r.Processed = prev.Processed
		}
		ids := strs(d.Data, "ids")
		switch kind {
		case KindApply:
			r.Accepted = ids
			r.Rejected = strs(d.Data, "rejected")
		default:
			var missing []string
			for _, id := range ids {
				nd, ok := notes[id]
				if !ok {
					missing = append(missing, id)
					continue
				}
				it := Item{Note: id, Anchor: str(nd.Data, "anchor"), Excerpt: str(nd.Data, "excerpt"),
					Text: str(nd.Data, "text"), Kind: str(nd.Data, "kind"), Value: str(nd.Data, "value"),
					Decision: str(nd.Data, "decision"), Doc: str(nd.Data, "doc")}
				if it.Anchor == "" {
					it.Anchor = str(nd.Data, "label")
				}
				if prev != nil {
					for _, p := range prev.Items {
						if p.Note == id {
							it.Disposition, it.Proposals, it.Why = p.Disposition, p.Proposals, p.Why
						}
					}
				}
				r.Items = append(r.Items, it)
			}
			if len(missing) > 0 {
				return nil, fmt.Errorf("round %d names notes the export lacks: %s (export the notes collection too)", n, strings.Join(missing, ", "))
			}
		}
		if err := SaveRound(sub, r); err != nil {
			return nil, err
		}
		if n > s.LastRound {
			s.LastRound = n
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Round < out[j].Round })
	if len(out) > 0 {
		if err := Save(sub, s); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func findItem(r *Round, note string) *Item {
	for i := range r.Items {
		if r.Items[i].Note == note {
			return &r.Items[i]
		}
	}
	return nil
}

// Dispose records what was done with a note: answered, page or declined.
// A change request gets its disposition from Propose.
func Dispose(sub Subject, n int, note, disposition, why string) error {
	switch disposition {
	case Answered, PageOnly, Declined:
	case Proposed:
		return errors.New("a change request is disposed by `bts review propose`, which records the proposal")
	default:
		return fmt.Errorf("disposition takes answered, page or declined, got %q", disposition)
	}
	if disposition == Declined && strings.TrimSpace(why) == "" {
		return errors.New("--why is required for declined: the reason the user reads")
	}
	r, err := LoadRound(sub, n)
	if err != nil {
		return fmt.Errorf("round %d is not imported: %w", n, err)
	}
	it := findItem(r, note)
	if it == nil {
		return fmt.Errorf("round %d has no note %s", n, note)
	}
	it.Disposition, it.Why = disposition, strings.TrimSpace(why)
	return SaveRound(sub, r)
}

func writeUpload(sub Subject, collection, id string, doc map[string]interface{}) (string, error) {
	dir := filepath.Join(UploadDir(sub), collection)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+".json")
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0644)
}

// Propose records a proposal made from a note and writes the page
// document for it.  It returns the proposal and the file to upload to the
// page's `proposals` collection (ArtifactData set, file_path).
func Propose(sub Subject, n int, p Proposal) (*Proposal, string, error) {
	if !contains(sub.Docs, p.Target) {
		return nil, "", fmt.Errorf("--target takes %s, got %q", strings.Join(sub.Docs, " or "), p.Target)
	}
	if strings.TrimSpace(p.Summary) == "" {
		return nil, "", errors.New("--summary is required: what changes, in a sentence the user reads")
	}
	r, err := LoadRound(sub, n)
	if err != nil {
		return nil, "", fmt.Errorf("round %d is not imported: %w", n, err)
	}
	it := findItem(r, p.Note)
	if it == nil {
		return nil, "", fmt.Errorf("round %d has no note %s", n, p.Note)
	}
	if p.Kind == "" {
		p.Kind = "edit"
	}
	if p.Anchor == "" {
		p.Anchor = it.Anchor
	}
	p.ID = fmt.Sprintf("p-%d-%d", n, len(r.Proposals)+1)
	p.Round, p.Status, p.Created = n, PStatusProposed, now()
	r.Proposals = append(r.Proposals, p)
	it.Disposition = Proposed
	it.Proposals = append(it.Proposals, p.ID)
	if err := SaveRound(sub, r); err != nil {
		return nil, "", err
	}
	doc := map[string]interface{}{
		"round": p.Round, "note": p.Note, "anchor": p.Anchor, "kind": p.Kind, "target": p.Target,
		"status": p.Status, "created": p.Created, "summary": p.Summary,
	}
	for k, v := range map[string]string{"before": p.Before, "after": p.After, "why": p.Why, "to_lane": p.ToLane, "to_label": p.ToLabel} {
		if v != "" {
			doc[k] = v
		}
	}
	path, err := writeUpload(sub, "proposals", p.ID, doc)
	return &p, path, err
}

// findProposal finds a proposal by id across the rounds.
func findProposal(rounds []*Round, id string) (*Round, int) {
	for _, r := range rounds {
		for i := range r.Proposals {
			if r.Proposals[i].ID == id {
				return r, i
			}
		}
	}
	return nil, -1
}

// Apply records an apply round once the session has edited the plan files
// for its accepted proposals: each target document gets a new version
// whose hash must differ from the last recorded one, accepted proposals
// become applied and rejected ones rejected.  It returns the revisions and
// the page documents to upload (proposals collection, ArtifactData update).
func Apply(sub Subject, n int) ([]Revision, []string, error) {
	s, err := Load(sub)
	if err != nil {
		return nil, nil, err
	}
	r, err := LoadRound(sub, n)
	if err != nil {
		return nil, nil, fmt.Errorf("round %d is not imported: %w", n, err)
	}
	if r.Kind != KindApply {
		return nil, nil, fmt.Errorf("round %d is a %s round, not an apply round", n, r.Kind)
	}
	rounds, err := Rounds(sub)
	if err != nil {
		return nil, nil, err
	}
	byTarget := map[string][]string{}
	for _, id := range r.Accepted {
		origin, i := findProposal(rounds, id)
		if origin == nil {
			return nil, nil, fmt.Errorf("accepted proposal %s is in no recorded round", id)
		}
		byTarget[origin.Proposals[i].Target] = append(byTarget[origin.Proposals[i].Target], id)
	}
	var revs []Revision
	targets := make([]string, 0, len(byTarget))
	for t := range byTarget {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, t := range targets {
		h, err := sub.HashDoc(t)
		if err != nil {
			return nil, nil, err
		}
		d := s.Docs[t]
		if d == nil {
			return nil, nil, fmt.Errorf("%s has no recorded version: run `bts review page` first", t)
		}
		if h == d.Hash {
			return nil, nil, fmt.Errorf("%s is unchanged since version %d (%s): edit it for %s first",
				t, d.Version, d.Hash, strings.Join(byTarget[t], ", "))
		}
		rev := Revision{File: t, Version: d.Version + 1, From: d.Hash, To: h, Round: n, Proposals: byTarget[t], At: now()}
		d.Version, d.Hash = rev.Version, h
		d.History = append(d.History, rev)
		revs = append(revs, rev)
	}
	// The bundle the page confirms moves with its documents: one new
	// version for the round, naming every proposal it applied.
	if sub.Bundle != "" && len(revs) > 0 {
		h, err := bundleHash(sub)
		if err != nil {
			return nil, nil, err
		}
		if rev := bump(s, sub.Bundle, h, Revision{Round: n, Proposals: r.Accepted}); rev != nil {
			revs = append(revs, *rev)
		}
	}
	var uploads []string
	mark := func(id, status string) error {
		origin, i := findProposal(rounds, id)
		if origin == nil {
			return nil
		}
		origin.Proposals[i].Status = status
		upd := map[string]interface{}{"status": status}
		if status == PStatusApplied {
			origin.Proposals[i].Applied = now()
			upd["applied"] = origin.Proposals[i].Applied
			for _, rev := range revs {
				if rev.File == origin.Proposals[i].Target && rev.File != sub.Bundle {
					upd["applied_in"] = fmt.Sprintf("%s v%d (%s)", rev.File, rev.Version, rev.To)
				}
			}
		}
		if err := SaveRound(sub, origin); err != nil {
			return err
		}
		path, err := writeUpload(sub, "proposals", id, upd)
		if err == nil {
			uploads = append(uploads, path)
		}
		return err
	}
	for _, id := range r.Accepted {
		if err := mark(id, PStatusApplied); err != nil {
			return nil, nil, err
		}
	}
	for _, id := range r.Rejected {
		if err := mark(id, PStatusRejected); err != nil {
			return nil, nil, err
		}
	}
	// The apply round itself may have been rewritten by mark (it holds no
	// proposals, so it was not); reload before recording the revisions.
	r, err = LoadRound(sub, n)
	if err != nil {
		return nil, nil, err
	}
	r.Revisions = revs
	if r.FilesAfter, err = currentHashes(sub); err != nil {
		return nil, nil, err
	}
	if err := SaveRound(sub, r); err != nil {
		return nil, nil, err
	}
	if err := Save(sub, s); err != nil {
		return nil, nil, err
	}
	if sub.Log != nil {
		for _, rev := range revs {
			sub.Log(rev)
		}
	}
	return revs, uploads, nil
}

// Done marks a round processed: every note of a review round has a
// disposition, and an apply round with accepted proposals has its
// revisions.  It returns the page document to upload (rounds collection,
// ArtifactData update).
func Done(sub Subject, n int) (string, error) {
	r, err := LoadRound(sub, n)
	if err != nil {
		return "", fmt.Errorf("round %d is not imported: %w", n, err)
	}
	var open []string
	for _, it := range r.Items {
		if it.Kind != KindConfirm && it.Disposition == "" {
			open = append(open, it.Note+" ("+it.Anchor+")")
		}
	}
	if len(open) > 0 {
		return "", fmt.Errorf("round %d has notes with no disposition: %s", n, strings.Join(open, ", "))
	}
	if r.Kind == KindApply && len(r.Accepted) > 0 && len(r.Revisions) == 0 {
		return "", fmt.Errorf("round %d accepted %s but recorded no revision: run `bts review apply` first", n, strings.Join(r.Accepted, ", "))
	}
	r.Processed = now()
	if r.FilesAfter == nil {
		if r.FilesAfter, err = currentHashes(sub); err != nil {
			return "", err
		}
	}
	if err := SaveRound(sub, r); err != nil {
		return "", err
	}
	return writeUpload(sub, "rounds", fmt.Sprintf("r-%d", n), map[string]interface{}{
		"status": "processed", "processed": r.Processed,
	})
}
