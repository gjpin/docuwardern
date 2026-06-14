package corpus

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSortDeduplicatesIdenticalReportEvents(t *testing.T) {
	duplicate := PageEvent{URL: "https://cloud.example.com/", Detail: "outside seed scope"}
	distinct := PageEvent{URL: duplicate.URL, Detail: "Markdown resource"}
	artifact := Artifact{Report: Report{Skipped: []PageEvent{duplicate, distinct, duplicate}}}

	Sort(&artifact)

	if len(artifact.Report.Skipped) != 2 {
		t.Fatalf("skipped = %+v, want two distinct events", artifact.Report.Skipped)
	}
	if artifact.Report.Skipped[0] != distinct || artifact.Report.Skipped[1] != duplicate {
		t.Fatalf("skipped = %+v, want sorted distinct events", artifact.Report.Skipped)
	}
}

func TestLegacyLinkSelectorsAreIgnoredAndRemovedOnRewrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "legacy")
	artifact := Artifact{Manifest: Manifest{SchemaVersion: SchemaVersion, Source: SourceSpec{SourceID: "source", SeedURL: "https://example.com/docs", ContentSelector: "main"}, Complete: true}, Markdown: map[string]string{}}
	if err := Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(data), `"seed_url": "https://example.com/docs",`, "\"seed_url\": \"https://example.com/docs\",\n    \"link_selectors\": [\"nav a\"],", 1)
	if err := os.WriteFile(manifestPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	read, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := filepath.Join(t.TempDir(), "rewritten")
	if err := Write(rewritten, read); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(rewritten, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "link_selectors") {
		t.Fatalf("legacy field retained:\n%s", data)
	}
}
