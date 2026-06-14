package qdrantstore

import (
	"strings"
	"testing"

	qdrant "github.com/qdrant/go-client/qdrant"
	"github.com/zero/docuwarden/vectorstore"
)

func TestDenseVectorDataSupportsCurrentAndLegacyResponses(t *testing.T) {
	current := &qdrant.VectorOutput{Vector: &qdrant.VectorOutput_Dense{Dense: &qdrant.DenseVector{Data: []float32{1, 2}}}}
	if got := denseVectorData(current); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("current dense vector = %#v", got)
	}
	legacy := &qdrant.VectorOutput{Data: []float32{3, 4}}
	if got := denseVectorData(legacy); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("legacy dense vector = %#v", got)
	}
}

func TestPhysicalCollectionNameIncludesSourceAndVersion(t *testing.T) {
	name := physicalName("Nuxt", "4.x")
	if !strings.HasPrefix(name, "nuxt__4_x__snapshot_") {
		t.Fatalf("name = %q", name)
	}
	if !strings.HasPrefix(name, collectionPrefix("Nuxt")) {
		t.Fatalf("name %q does not match cleanup prefix", name)
	}
}

func TestPhysicalCollectionNameIncludesUnversionedLabel(t *testing.T) {
	name := physicalName("Nuxt Docs", "")
	if !strings.HasPrefix(name, "nuxt_docs__unversioned__snapshot_") {
		t.Fatalf("name = %q", name)
	}
}

func TestDocumentPointIDIsDeterministicAndDistinct(t *testing.T) {
	base := vectorstore.DocumentPointID("nuxt", "https://nuxt.com/docs/guide")
	if base != vectorstore.DocumentPointID("nuxt", "https://nuxt.com/docs/guide") {
		t.Fatal("document ID changed for identical input")
	}
	if base == vectorstore.DocumentPointID("vue", "https://nuxt.com/docs/guide") || base == vectorstore.DocumentPointID("nuxt", "https://nuxt.com/docs/api") {
		t.Fatal("document ID collided across source or URL")
	}
}
