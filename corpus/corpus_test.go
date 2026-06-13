package corpus

import (
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactRoundTrip(t *testing.T) {
	id := DocumentID("source", "v1", "https://example.com/docs")
	body := "# Docs\n"
	artifact := Artifact{Manifest: Manifest{SchemaVersion: SchemaVersion, Source: SourceSpec{SourceID: "source", SeedURL: "https://example.com/docs", ContentSelector: "main", Version: "v1"}, Complete: true, Documents: []Document{{ID: id, URL: "https://example.com/docs", Filename: "documents/" + FilenameFor(id), ContentHash: HashString(body), CrawledAt: time.Unix(1, 0).UTC()}}}, Markdown: map[string]string{id: body}}
	dir := t.TempDir()
	if err := Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	read, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if read.Markdown[id] != body || read.Manifest.Documents[0].ID != id {
		t.Fatalf("round trip mismatch: %+v", read)
	}
}

func TestFailedWritePreservesExistingArtifact(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "artifact")
	body := "original"
	id := DocumentID("source", "", "https://example.com/docs")
	original := Artifact{Manifest: Manifest{SchemaVersion: SchemaVersion, Source: SourceSpec{SourceID: "source", SeedURL: "https://example.com/docs", ContentSelector: "main"}, Complete: true, Documents: []Document{{ID: id, URL: "https://example.com/docs", Filename: "documents/" + FilenameFor(id), ContentHash: HashString(body)}}}, Markdown: map[string]string{id: body}}
	if err := Write(dir, original); err != nil {
		t.Fatal(err)
	}
	broken := original
	broken.Markdown = map[string]string{}
	if err := Write(dir, broken); err == nil {
		t.Fatal("expected failed write")
	}
	read, err := Read(dir)
	if err != nil {
		t.Fatalf("original artifact damaged: %v", err)
	}
	if read.Markdown[id] != body {
		t.Fatalf("body = %q", read.Markdown[id])
	}
}

func TestDocumentIDIncludesVersionAndURL(t *testing.T) {
	a := DocumentID("s", "v1", "https://x/a")
	if a == DocumentID("s", "v2", "https://x/a") || a == DocumentID("s", "v1", "https://x/b") {
		t.Fatal("document ID collision")
	}
}
