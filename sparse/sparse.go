package sparse

import (
	"hash/fnv"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type Vector struct {
	Indices []uint32
	Values  []float32
}

type Encoder interface {
	Encode(text string) Vector
}

type LexicalEncoder struct{}

var tokenPattern = regexp.MustCompile(`[\pL\pN][\pL\pN_.:/@-]*`)

func (LexicalEncoder) Encode(text string) Vector {
	counts := map[uint32]int{}
	for _, token := range tokens(text) {
		counts[hashToken(token)]++
	}
	indices := make([]uint32, 0, len(counts))
	for index := range counts {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	values := make([]float32, len(indices))
	for i, index := range indices {
		values[i] = float32(1 + math.Log(float64(counts[index])))
	}
	return Vector{Indices: indices, Values: values}
}

func tokens(text string) []string {
	var result []string
	for _, raw := range tokenPattern.FindAllString(text, -1) {
		lower := strings.ToLower(raw)
		result = append(result, lower)
		for _, part := range splitIdentifier(raw) {
			part = strings.ToLower(part)
			if len(part) > 1 && part != lower {
				result = append(result, part)
			}
		}
	}
	return result
}

func splitIdentifier(value string) []string {
	var parts []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			parts = append(parts, string(current))
			current = nil
		}
	}
	runes := []rune(value)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(current) > 0 && unicode.IsUpper(r) && (unicode.IsLower(current[len(current)-1]) || i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
			flush()
		}
		current = append(current, r)
	}
	flush()
	return parts
}

func hashToken(token string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(token))
	return hash.Sum32()
}
