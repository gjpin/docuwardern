package sparse

import "testing"

func TestLexicalEncoderPreservesIdentifiersAndTerms(t *testing.T) {
	vector := (LexicalEncoder{}).Encode("useRuntimeConfig NUXT_PUBLIC_API useRuntimeConfig")
	if len(vector.Indices) < 5 {
		t.Fatalf("too few terms: %+v", vector)
	}
	for i := 1; i < len(vector.Indices); i++ {
		if vector.Indices[i] <= vector.Indices[i-1] {
			t.Fatal("indices are not sorted and unique")
		}
	}
}

func TestLexicalEncoderIsDeterministic(t *testing.T) {
	a := (LexicalEncoder{}).Encode("defineNuxtPlugin")
	b := (LexicalEncoder{}).Encode("defineNuxtPlugin")
	if len(a.Indices) != len(b.Indices) {
		t.Fatal("length mismatch")
	}
	for i := range a.Indices {
		if a.Indices[i] != b.Indices[i] || a.Values[i] != b.Values[i] {
			t.Fatal("encoding changed")
		}
	}
}
