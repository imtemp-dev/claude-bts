package template

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
	"sync"
)

// TemplateHashFile is where a project records the hash of the templates
// it was deployed from, beside .template-version.
const TemplateHashFile = ".bts/config/.template-hash"

var (
	templatesHashOnce  sync.Once
	templatesHashValue string
)

// TemplatesHash fingerprints this binary's embedded templates: every path
// and its content, in sorted order.  The template version is the release
// plus the commit, so a build from a tree with uncommitted template
// changes keeps the version; the hash is what tells its templates apart
// (trial problem #15, AGEN-65).  It is "" when the templates cannot be
// read, which Update treats as unknown.
func TemplatesHash() string {
	templatesHashOnce.Do(func() {
		fsys, err := EmbeddedTemplates()
		if err != nil {
			return
		}
		var paths []string
		if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				paths = append(paths, p)
			}
			return nil
		}); err != nil {
			return
		}
		sort.Strings(paths)
		h := sha256.New()
		for _, p := range paths {
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return
			}
			h.Write([]byte(p))
			h.Write([]byte{0})
			h.Write(data)
			h.Write([]byte{0})
		}
		templatesHashValue = hex.EncodeToString(h.Sum(nil))[:16]
	})
	return templatesHashValue
}
