package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func exerciseOwnerTokenDatabaseLifecycle(t *testing.T) {
	t.Helper()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	primary, err := CreateOwnerWithPrimary("owner-1", "hash-primary", "own_primary", "mary", "enc-primary", now)
	if err != nil {
		t.Fatal(err)
	}
	if primary.Kind != "primary" || primary.OwnerID != "owner-1" || primary.CreatedAt != now.Unix() {
		t.Fatalf("primary token = %+v", primary)
	}
	if last4, err := OwnerTokenLast4("owner-1"); err != nil || last4 != "mary" {
		t.Fatalf("OwnerTokenLast4 = %q, %v", last4, err)
	}
	if _, err := CreateOwnerWithPrimary("owner-1", "duplicate", "own_duplicate", "cate", "enc", now); err == nil {
		t.Fatal("duplicate owner creation succeeded")
	}

	expires := now.Add(24 * time.Hour).Unix()
	linked, err := CreateLinkedOwnerToken("owner-1", "automation", "hash-linked", "own_linked", "nked", "enc-linked", &expires, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Kind != "linked" || !linked.Name.Valid || linked.Name.String != "automation" || !linked.ExpiresAt.Valid {
		t.Fatalf("linked token = %+v", linked)
	}
	if _, err := CreateLinkedOwnerToken("owner-1", "second", "hash-2", "own_second", "cond", "enc-2", nil, 1, now); err == nil {
		t.Fatal("linked token limit was not enforced")
	}
	if _, err := CreateLinkedOwnerToken("missing-owner", "orphan", "hash-x", "own_orphan", "phan", "enc-x", nil, 0, now); !errors.Is(err, ErrOwnerTokenNotFound) {
		t.Fatalf("orphan create error = %v", err)
	}

	byPrefix, err := FindOwnerTokensByPrefix("own_primary")
	if err != nil || len(byPrefix) != 1 || byPrefix[0].ID != primary.ID {
		t.Fatalf("FindOwnerTokensByPrefix = %+v, %v", byPrefix, err)
	}
	if _, err := GetOwnerToken("wrong-owner", primary.ID); !errors.Is(err, ErrOwnerTokenNotFound) {
		t.Fatalf("wrong-owner lookup error = %v", err)
	}
	if got, err := GetOwnerToken("owner-1", primary.ID); err != nil || got.TokenHash != "hash-primary" {
		t.Fatalf("GetOwnerToken = %+v, %v", got, err)
	}
	if got, err := ListOwnerTokens("owner-1"); err != nil || len(got) != 2 || got[0].Kind != "primary" {
		t.Fatalf("ListOwnerTokens = %+v, %v", got, err)
	}
	if count, err := CountActiveLinkedTokens("owner-1", now); err != nil || count != 1 {
		t.Fatalf("CountActiveLinkedTokens = %d, %v", count, err)
	}

	if err := MarkOwnerTokenUsed(primary.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := MarkOwnerTokenUsed(primary.ID, now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := RotateOwnerToken("owner-1", primary.ID, "hash-rotated", "own_rotated", "ated", "enc-rotated", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if last4, err := OwnerTokenLast4("owner-1"); err != nil || last4 != "ated" {
		t.Fatalf("rotated OwnerTokenLast4 = %q, %v", last4, err)
	}
	if err := RotateOwnerToken("owner-1", 9999, "hash-missing", "own_missing", "sing", "enc-missing", now); !errors.Is(err, ErrOwnerTokenNotFound) {
		t.Fatalf("missing token rotation error = %v", err)
	}
	if err := UpdateLinkedOwnerToken("owner-1", linked.ID, "renamed", nil); err != nil {
		t.Fatal(err)
	}
	if err := UpdateLinkedOwnerToken("owner-1", primary.ID, "invalid", nil); !errors.Is(err, ErrOwnerTokenNotFound) {
		t.Fatalf("update primary error = %v", err)
	}
	if err := RevokeLinkedOwnerToken("owner-1", linked.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := RevokeLinkedOwnerToken("owner-1", linked.ID, now.Add(3*time.Hour)); !errors.Is(err, ErrOwnerTokenNotFound) {
		t.Fatalf("duplicate linked revoke error = %v", err)
	}
	if count, err := CountActiveLinkedTokens("owner-1", now.Add(2*time.Hour)); err != nil || count != 0 {
		t.Fatalf("revoked linked count = %d, %v", count, err)
	}
	if count, err := RevokeOwnerTokenPrefix("own_rotated", now.Add(3*time.Hour)); err != nil || count != 2 {
		t.Fatalf("primary prefix revoke deleted tokens = %d, %v", count, err)
	}
	if tokens, err := ListOwnerTokens("owner-1"); err != nil || len(tokens) != 0 {
		t.Fatalf("primary revoke did not delete owner tokens: %+v, %v", tokens, err)
	}
	if _, err := GetOwner("owner-1"); !errors.Is(err, ErrOwnerTokenNotFound) {
		t.Fatalf("primary revoke did not delete owner: %v", err)
	}
	if _, err := RevokeOwnerTokenPrefix("unknown", now); !errors.Is(err, ErrOwnerTokenNotFound) {
		t.Fatalf("unknown prefix revoke error = %v", err)
	}
	linkedPrimary, err := CreateOwnerWithPrimary("owner-2", "hash-2", "own_2", "wner", "enc-2", now)
	if err != nil {
		t.Fatal(err)
	}
	operatorRevoked, err := CreateLinkedOwnerToken("owner-2", "operator", "hash-operator", "own_operator", "ator", "enc-operator", nil, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := RevokeOwnerTokenPrefix("own_operator", now.Add(4*time.Hour)); err != nil || count != 1 {
		t.Fatalf("linked prefix revoke = %d, %v (token %+v)", count, err, operatorRevoked)
	}

	if _, err := CreateOwnerWithPrimary("owner-3", "hash-3", "own_3", "wner", "enc-3", now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := MarkOwnerTokenUsed(linkedPrimary.ID, now.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := FlushHotCounters(context.Background()); err != nil {
		t.Fatal(err)
	}
	removed, err := PurgeIdleOwners(context.Background(), now.Add(-24*time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("PurgeIdleOwners = %d, %v", removed, err)
	}
	if tokens, err := ListOwnerTokens("owner-3"); err != nil || len(tokens) != 0 {
		t.Fatalf("purged owner still has tokens: %+v, %v", tokens, err)
	}
}

func TestOwnerTokenDatabaseRequiresInitialization(t *testing.T) {
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerTokenDB(); err == nil {
		t.Fatal("owner token database returned a connection before initialization")
	}
}
