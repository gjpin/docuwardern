package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

type SourceSpec struct {
	SourceID        string   `json:"source_id"`
	SeedURL         string   `json:"seed_url"`
	LinkSelectors   []string `json:"link_selectors,omitempty"`
	ContentSelector string   `json:"content_selector"`
	Version         string   `json:"version,omitempty"`
}

type Document struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Title       string    `json:"title,omitempty"`
	Filename    string    `json:"filename"`
	ContentHash string    `json:"content_hash"`
	CrawledAt   time.Time `json:"crawled_at"`
}

type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Source        SourceSpec `json:"source"`
	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   time.Time  `json:"completed_at"`
	Complete      bool       `json:"complete"`
	Documents     []Document `json:"documents"`
}

type PageEvent struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Target     string `json:"target,omitempty"`
}

type Report struct {
	Fetched         []PageEvent `json:"fetched,omitempty"`
	Redirected      []PageEvent `json:"redirected,omitempty"`
	Skipped         []PageEvent `json:"skipped,omitempty"`
	Failed          []PageEvent `json:"failed,omitempty"`
	SelectorMissing []PageEvent `json:"selector_missing,omitempty"`
}

type Artifact struct {
	Manifest Manifest
	Report   Report
	Markdown map[string]string
}

func HashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func DocumentID(source, version, pageURL string) string {
	return HashString(strings.Join([]string{source, version, pageURL}, "\x00"))
}

func FilenameFor(id string) string { return id[:24] + ".md" }

func Write(dir string, artifact Artifact) error {
	if dir == "" {
		return errors.New("artifact output directory is required")
	}
	documentsDir := filepath.Join(dir, "documents")
	if err := os.RemoveAll(documentsDir); err != nil {
		return fmt.Errorf("clean artifact documents: %w", err)
	}
	if err := os.MkdirAll(documentsDir, 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	for _, doc := range artifact.Manifest.Documents {
		body, ok := artifact.Markdown[doc.ID]
		if !ok {
			return fmt.Errorf("markdown missing for document %s", doc.ID)
		}
		if err := os.WriteFile(filepath.Join(dir, doc.Filename), []byte(body), 0o644); err != nil {
			return fmt.Errorf("write document %s: %w", doc.URL, err)
		}
	}
	if err := writeJSON(filepath.Join(dir, "manifest.json"), artifact.Manifest); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "report.json"), artifact.Report)
}

func Read(dir string) (Artifact, error) {
	var artifact Artifact
	if err := readJSON(filepath.Join(dir, "manifest.json"), &artifact.Manifest); err != nil {
		return Artifact{}, err
	}
	if artifact.Manifest.SchemaVersion != SchemaVersion {
		return Artifact{}, fmt.Errorf("unsupported artifact schema version %d", artifact.Manifest.SchemaVersion)
	}
	if err := readJSON(filepath.Join(dir, "report.json"), &artifact.Report); err != nil {
		return Artifact{}, err
	}
	artifact.Markdown = make(map[string]string, len(artifact.Manifest.Documents))
	for _, doc := range artifact.Manifest.Documents {
		body, err := os.ReadFile(filepath.Join(dir, doc.Filename))
		if err != nil {
			return Artifact{}, fmt.Errorf("read document %s: %w", doc.URL, err)
		}
		if HashString(string(body)) != doc.ContentHash {
			return Artifact{}, fmt.Errorf("content hash mismatch for %s", doc.URL)
		}
		artifact.Markdown[doc.ID] = string(body)
	}
	return artifact, nil
}

func Sort(artifact *Artifact) {
	sort.Slice(artifact.Manifest.Documents, func(i, j int) bool { return artifact.Manifest.Documents[i].URL < artifact.Manifest.Documents[j].URL })
	for _, events := range [][]PageEvent{artifact.Report.Fetched, artifact.Report.Redirected, artifact.Report.Skipped, artifact.Report.Failed, artifact.Report.SelectorMissing} {
		sort.Slice(events, func(i, j int) bool { return events[i].URL < events[j].URL })
	}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}
