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

// formatVersion is the notes format the appended orphan is written in: it
// carries an audience and a source, which arrived with version 2. Files of the
// versions before it are read and upgraded; a newer one is refused, because
// rewriting it would drop what this build does not know.
const formatVersion = 2

// The CLI cannot add a comment without reading its target line. Preserve the
// rest of its document verbatim and append an orphan, addressed to the agent and
// the forge and remembering where it came from. It returns the new comment's id,
// so replies can be attached to it.
func appendOrphan(ctx context.Context, dir string, note forge.Note, content, author, sourceURL string) (string, error) {
	branch := ""
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	cmd.Dir = dir
	if out, err := cmd.Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	} else if ctx.Err() != nil {
		return "", ctx.Err()
	}
	name := "notes.json"
	if branch != "" {
		name = "notes_" + strings.ReplaceAll(branch, "/", "_") + ".json"
	}
	path := filepath.Join(dir, ".incomm", name)
	document := map[string]json.RawMessage{"version": json.RawMessage("2")}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &document); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	version := 1
	if raw, ok := document["version"]; ok {
		if err := json.Unmarshal(raw, &version); err != nil {
			return "", fmt.Errorf("unsupported incomm notes version; update the integration before importing orphaned comments")
		}
	}
	if version < 1 || version > formatVersion {
		return "", fmt.Errorf("unsupported incomm notes version; update the integration before importing orphaned comments")
	}
	var notes []json.RawMessage
	if data := document["notes"]; len(data) > 0 {
		if err := json.Unmarshal(data, &notes); err != nil {
			return "", err
		}
	}
	var raw8 [8]byte
	if _, err := rand.Read(raw8[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw8[:])
	now := time.Now().UTC().Format(time.RFC3339)
	// No current source text exists for this location. A newline cannot match a
	// trimmed source line and is too weak for Incomm's anchor search. Unlike an
	// empty prefix, it cannot accidentally attach the orphan to a blank line.
	entry := map[string]any{
		"id": id, "file": filepath.ToSlash(note.Path),
		"startLine": note.Line, "endLine": note.Line,
		"anchor":  map[string]string{"startPrefix": "\n", "endPrefix": "", "contextBefore": "", "contextAfter": "", "checksum": ""},
		"content": content, "resolved": note.Resolved, "orphaned": true,
		"author": "user", "authorTitle": author, "audience": "agent+external",
		"createdAt": now, "updatedAt": now,
		"replies": []any{},
	}
	// Left out when empty, as the CLI does, so the file reads the same
	// whoever wrote it.
	source := map[string]any{}
	if sourceURL != "" {
		source["url"] = sourceURL
	}
	if note.ID != 0 {
		source["id"] = note.ID
	}
	if note.Thread != "" {
		source["thread"] = note.Thread
	}
	if len(source) > 0 {
		entry["source"] = source
	}
	if !note.CreatedAt.IsZero() {
		entry["createdAt"] = note.CreatedAt.UTC().Format(time.RFC3339)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	notes = append(notes, raw)
	document["notes"], err = json.Marshal(notes)
	if err != nil {
		return "", err
	}
	// The orphan carries fields of the version it is written in.
	document["version"] = json.RawMessage("2")
	if branch != "" {
		document["branch"], _ = json.Marshal(branch)
	}
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".unagit-orphan-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return id, nil
}
