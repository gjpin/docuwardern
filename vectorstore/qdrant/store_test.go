package qdrantstore

import (
	"strings"
	"testing"
)

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
