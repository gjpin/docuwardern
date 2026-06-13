package chunk

import (
	"strings"
	"testing"
)

func TestSplitPreservesHeadingsOverlapAndFence(t *testing.T) {
	markdown := "# API\n\nIntro paragraph with enough words to form a block.\n\n## Run\n\n```go\n" + strings.Repeat("fmt.Println(1)\n", 40) + "```\n\nAfter code.\n"
	chunks, err := Split(markdown, Config{MaxTokens: 35, OverlapTokens: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	foundFence := false
	for i, item := range chunks {
		if item.Index != i {
			t.Fatalf("index = %d", item.Index)
		}
		if strings.Contains(item.Markdown, "```go") {
			if !strings.Contains(item.Markdown, "```\n") {
				t.Fatal("split fenced code")
			}
			foundFence = true
		}
	}
	if !foundFence {
		t.Fatal("fence missing")
	}
}

func TestSplitRejectsInvalidOverlap(t *testing.T) {
	if _, err := Split("text", Config{MaxTokens: 10, OverlapTokens: 10}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSplitBreaksOversizedProse(t *testing.T) {
	chunks, err := Split("# Long\n\n"+strings.Repeat("documentation word ", 100), Config{MaxTokens: 20, OverlapTokens: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 3 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, item := range chunks {
		if !strings.Contains(strings.Join(item.HeadingPath, "/"), "Long") {
			t.Fatalf("heading path = %v", item.HeadingPath)
		}
	}
}
