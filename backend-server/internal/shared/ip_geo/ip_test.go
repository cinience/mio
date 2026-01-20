package ip_geo

import "testing"

func TestCheckIP(t *testing.T) {
	searcher := NewSearcher()
	geo, err := searcher.Lookup("47.116.112.99")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if geo == nil {
		t.Fatalf("expected geo result, got nil")
	}
	t.Logf("geo: %+v", geo)
}
