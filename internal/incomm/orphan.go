package incomm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// The CLI cannot add a comment without reading its target line. Preserve the
// rest of its document verbatim and append an orphan using its version 1 schema.
func appendOrphan(ctx context.Context, dir string, note forge.Note, content, author string) error {
	branch := ""
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	cmd.Dir = dir
	if out, err := cmd.Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	name := "notes.json"
	if branch != "" {
		name = "notes_" + strings.ReplaceAll(branch, "/", "_") + ".json"
	}
	path := filepath.Join(dir, ".incomm", name)
	document := map[string]json.RawMessage{"version": json.RawMessage("1")}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	var version int
	if err := json.Unmarshal(document["version"], &version); err != nil || version != 1 {
		return fmt.Errorf("unsupported incomm notes version; update the integration before importing orphaned comments")
	}
	var notes []json.RawMessage
	if data := document["notes"]; len(data) > 0 {
		if err := json.Unmarshal(data, &notes); err != nil {
			return err
		}
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// No current source text exists for this location. A newline cannot match a
	// trimmed source line and is too weak for Incomm's anchor search. Unlike an
	// empty prefix, it cannot accidentally attach the orphan to a blank line.
	entry := map[string]any{
		"id": hex.EncodeToString(id[:]), "file": filepath.ToSlash(note.Path),
		"startLine": note.Line, "endLine": note.Line,
		"anchor":  map[string]string{"startPrefix": "\n", "endPrefix": "", "contextBefore": "", "contextAfter": "", "checksum": ""},
		"content": content, "resolved": note.Resolved, "orphaned": true,
		"author": "user", "authorTitle": author, "createdAt": now, "updatedAt": now,
		"replies": []any{},
	}
	if !note.CreatedAt.IsZero() {
		entry["createdAt"] = note.CreatedAt.UTC().Format(time.RFC3339)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	notes = append(notes, raw)
	document["notes"], err = json.Marshal(notes)
	if err != nil {
		return err
	}
	if branch != "" {
		document["branch"], _ = json.Marshal(branch)
	}
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".unagit-orphan-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
