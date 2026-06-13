package scrape

import "testing"

func TestScopeBoundaryAndCanonicalization(t *testing.T) {
	scope, seed, err := NewScope("https://EXAMPLE.com:443/docs/4.x/#top")
	if err != nil {
		t.Fatal(err)
	}
	if seed != "https://example.com/docs/4.x/" {
		t.Fatalf("seed = %q", seed)
	}
	tests := []struct {
		href     string
		accepted bool
		want     string
	}{
		{"guide/../guide/start#x", true, "https://example.com/docs/4.x/guide/start"},
		{"/docs/4.x", true, "https://example.com/docs/4.x"},
		{"/docs/4.x-old", false, "https://example.com/docs/4.x-old"},
		{"/docs/3.x", false, "https://example.com/docs/3.x"},
		{"https://other.example/docs/4.x", false, "https://other.example/docs/4.x"},
	}
	for _, test := range tests {
		got, accepted, err := scope.Resolve(seed+"/", test.href)
		if err != nil || got != test.want || accepted != test.accepted {
			t.Errorf("Resolve(%q) = %q, %v, %v", test.href, got, accepted, err)
		}
	}
}
