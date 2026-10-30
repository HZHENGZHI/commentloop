package server

import "testing"

func TestLocalTreeCacheInvalidation(t *testing.T) {
	var cache localTreeCache
	ro := &root{ID: "r"}
	calls := 0
	list := func(*root) ([]fileEntry, bool, error) {
		calls++
		return []fileEntry{{Path: "r/a.md"}}, false, nil
	}
	for i := 0; i < 3; i++ {
		if files, _, _ := cache.get(ro, list); len(files) != 1 {
			t.Fatalf("got %d files", len(files))
		}
	}
	if calls != 1 {
		t.Fatalf("expected 1 walk, got %d", calls)
	}
	cache.invalidate(ro)
	cache.get(ro, list)
	if calls != 2 {
		t.Fatalf("expected re-walk after invalidate, got %d walks", calls)
	}
}

func TestLocalTreeCacheDropsListingRacingInvalidation(t *testing.T) {
	var cache localTreeCache
	ro := &root{ID: "r"}
	calls := 0
	list := func(*root) ([]fileEntry, bool, error) {
		calls++
		if calls == 1 {
			cache.invalidate(ro)
		}
		return nil, false, nil
	}
	cache.get(ro, list)
	cache.get(ro, list)
	if calls != 2 {
		t.Fatalf("stale listing was cached: %d walks", calls)
	}
}

func TestLocalTreeCacheDropsListingRacingForget(t *testing.T) {
	var cache localTreeCache
	ro := &root{ID: "r"}
	cache.get(ro, func(*root) ([]fileEntry, bool, error) {
		cache.forget(ro)
		return []fileEntry{{Path: "r/a.md"}}, false, nil
	})
	if len(cache.entries) != 0 || len(cache.gen) != 0 {
		t.Fatalf("forgotten root retained: entries=%d gen=%d", len(cache.entries), len(cache.gen))
	}
}
