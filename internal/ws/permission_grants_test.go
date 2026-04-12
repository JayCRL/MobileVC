package ws

import (
	"testing"
	"time"
)

func TestPermissionGrantStoreConsumeIfValid(t *testing.T) {
	store := newPermissionGrantStore()
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	if !store.Issue("s1", "./README.md", time.Minute) {
		t.Fatal("expected issue success")
	}
	if !store.ConsumeIfValid("s1", "README.md") {
		t.Fatal("expected first consume success")
	}
	if store.ConsumeIfValid("s1", "README.md") {
		t.Fatal("expected second consume to fail")
	}
}

func TestPermissionGrantStoreRejectsExpiredGrant(t *testing.T) {
	store := newPermissionGrantStore()
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	if !store.Issue("s1", "README.md", time.Second) {
		t.Fatal("expected issue success")
	}
	now = now.Add(2 * time.Second)
	if store.ConsumeIfValid("s1", "README.md") {
		t.Fatal("expected expired grant to fail")
	}
}

func TestPermissionGrantStoreRejectsDifferentSessionOrPath(t *testing.T) {
	store := newPermissionGrantStore()
	if !store.Issue("s1", "README.md", time.Minute) {
		t.Fatal("expected issue success")
	}
	if store.ConsumeIfValid("s2", "README.md") {
		t.Fatal("expected different session to fail")
	}
	if store.ConsumeIfValid("s1", "OTHER.md") {
		t.Fatal("expected different path to fail")
	}
	if !store.ConsumeIfValid("s1", "README.md") {
		t.Fatal("expected original grant to remain consumable")
	}
}

func TestPermissionGrantStorePathIsolationStrictBinding(t *testing.T) {
	store := newPermissionGrantStore()
	if !store.Issue("s1", "a.go", time.Minute) {
		t.Fatal("expected issue success")
	}
	if store.ConsumeIfValid("s1", "b.go") {
		t.Fatal("expected different file path to fail")
	}
	if !store.ConsumeIfValid("s1", "a.go") {
		t.Fatal("expected approved path to remain consumable")
	}
}

func TestPermissionGrantStoreTimeoutExpiryAtSixtyOneSeconds(t *testing.T) {
	store := newPermissionGrantStore()
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if !store.Issue("s1", "a.go", 60*time.Second) {
		t.Fatal("expected issue success")
	}
	now = now.Add(61 * time.Second)
	if store.ConsumeIfValid("s1", "a.go") {
		t.Fatal("expected grant to expire after 61 seconds")
	}
}
