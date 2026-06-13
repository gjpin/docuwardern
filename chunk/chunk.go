package chunk

import (
	"fmt"
	"regexp"
	"strings"
)

type Config struct {
	MaxTokens     int
	OverlapTokens int
}

type Chunk struct {
	Index       int
	HeadingPath []string
	Markdown    string
}

type block struct {
	text     string
	headings []string
	tokens   int
}

var headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

func Split(markdown string, cfg Config) ([]Chunk, error) {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 800
	}
	if cfg.OverlapTokens < 0 || cfg.OverlapTokens >= cfg.MaxTokens {
		return nil, fmt.Errorf("overlap tokens must be between 0 and max tokens")
	}
	if cfg.OverlapTokens == 0 {
		cfg.OverlapTokens = 100
	}
	blocks := parseBlocks(markdown)
	blocks = splitOversizedProse(blocks, cfg.MaxTokens)
	if len(blocks) == 0 {
		return nil, nil
	}
	var chunks []Chunk
	var current []block
	currentTokens := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		var parts []string
		for _, item := range current {
			parts = append(parts, item.text)
		}
		chunks = append(chunks, Chunk{Index: len(chunks), HeadingPath: append([]string(nil), current[len(current)-1].headings...), Markdown: strings.TrimSpace(strings.Join(parts, "\n\n")) + "\n"})
		if cfg.OverlapTokens == 0 || current[len(current)-1].tokens > cfg.MaxTokens {
			current = nil
			currentTokens = 0
			return
		}
		kept := current[:0]
		keptTokens := 0
		for i := len(current) - 1; i >= 0 && keptTokens < cfg.OverlapTokens; i-- {
			kept = append([]block{current[i]}, kept...)
			keptTokens += current[i].tokens
		}
		current = kept
		currentTokens = keptTokens
	}
	for _, item := range blocks {
		if len(current) == 1 && current[0].tokens > cfg.MaxTokens {
			current = nil
			currentTokens = 0
		}
		if len(current) > 0 && currentTokens+item.tokens > cfg.MaxTokens {
			flush()
		}
		current = append(current, item)
		currentTokens += item.tokens
		if item.tokens > cfg.MaxTokens {
			flush()
		}
	}
	flush()
	return chunks, nil
}

func splitOversizedProse(blocks []block, maxTokens int) []block {
	var expanded []block
	for _, item := range blocks {
		trimmed := strings.TrimSpace(item.text)
		if item.tokens <= maxTokens || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			expanded = append(expanded, item)
			continue
		}
		words := strings.Fields(item.text)
		var current []string
		for _, word := range words {
			candidate := strings.Join(append(current, word), " ")
			if len(current) > 0 && estimateTokens(candidate) > maxTokens {
				text := strings.Join(current, " ")
				expanded = append(expanded, block{text: text, headings: append([]string(nil), item.headings...), tokens: estimateTokens(text)})
				current = nil
			}
			current = append(current, word)
		}
		if len(current) > 0 {
			text := strings.Join(current, " ")
			expanded = append(expanded, block{text: text, headings: append([]string(nil), item.headings...), tokens: estimateTokens(text)})
		}
	}
	return expanded
}

func parseBlocks(markdown string) []block {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	headings := make([]string, 0, 6)
	var blocks []block
	var current []string
	inFence := false
	flush := func() {
		text := strings.TrimSpace(strings.Join(current, "\n"))
		if text != "" {
			blocks = append(blocks, block{text: text, headings: append([]string(nil), headings...), tokens: estimateTokens(text)})
		}
		current = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			current = append(current, line)
			continue
		}
		if !inFence {
			if match := headingPattern.FindStringSubmatch(line); match != nil {
				flush()
				level := len(match[1])
				if len(headings) >= level {
					headings = headings[:level-1]
				}
				for len(headings) < level-1 {
					headings = append(headings, "")
				}
				headings = append(headings, strings.TrimSpace(match[2]))
				current = append(current, line)
				continue
			}
			if trimmed == "" {
				flush()
				continue
			}
		}
		current = append(current, line)
	}
	flush()
	return blocks
}

func estimateTokens(text string) int {
	count := (len([]rune(text)) + 3) / 4
	if count < 1 {
		return 1
	}
	return count
}
