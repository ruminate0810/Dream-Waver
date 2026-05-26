package store

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// newUUIDTestMap returns a pgtype.Map with our codec registered —
// every test gets a fresh Map so registration side-effects don't leak.
func newUUIDTestMap() *pgtype.Map {
	m := pgtype.NewMap()
	registerUUIDCodec(m)
	return m
}

func TestRegisterUUIDCodec_BinaryRoundTrip(t *testing.T) {
	t.Parallel()
	m := newUUIDTestMap()
	src := uuid.New()

	encoded, err := m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, src, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Equal(encoded, src[:]) {
		t.Fatalf("binary encoded bytes != raw uuid bytes: got %x want %x", encoded, src[:])
	}

	var got uuid.UUID
	if err := m.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, encoded, &got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != src {
		t.Fatalf("round-trip mismatch: got %s want %s", got, src)
	}
}

func TestRegisterUUIDCodec_TextRoundTrip(t *testing.T) {
	t.Parallel()
	m := newUUIDTestMap()
	src := uuid.New()

	encoded, err := m.Encode(pgtype.UUIDOID, pgtype.TextFormatCode, src, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != src.String() {
		t.Fatalf("text encoded != uuid.String(): got %q want %q", encoded, src.String())
	}

	var got uuid.UUID
	if err := m.Scan(pgtype.UUIDOID, pgtype.TextFormatCode, encoded, &got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != src {
		t.Fatalf("round-trip mismatch: got %s want %s", got, src)
	}
}

func TestRegisterUUIDCodec_NullUUID_Valid(t *testing.T) {
	t.Parallel()
	m := newUUIDTestMap()
	src := uuid.NullUUID{UUID: uuid.New(), Valid: true}

	encoded, err := m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, src, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var got uuid.NullUUID
	if err := m.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, encoded, &got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !got.Valid || got.UUID != src.UUID {
		t.Fatalf("round-trip: got %+v want %+v", got, src)
	}
}

func TestRegisterUUIDCodec_NullUUID_NULL(t *testing.T) {
	t.Parallel()
	m := newUUIDTestMap()
	src := uuid.NullUUID{Valid: false}

	encoded, err := m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, src, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// NULL must wire as a zero-length / nil payload.
	if encoded != nil {
		t.Fatalf("NULL NullUUID should encode as nil, got %x", encoded)
	}

	got := uuid.NullUUID{UUID: uuid.New(), Valid: true} // pre-populate to prove we overwrite
	if err := m.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, nil, &got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got.Valid {
		t.Fatalf("scanning NULL should set Valid=false, got %+v", got)
	}
}

func TestRegisterUUIDCodec_NullInto_UUID_Errors(t *testing.T) {
	t.Parallel()
	m := newUUIDTestMap()

	var got uuid.UUID
	err := m.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, nil, &got)
	if err == nil {
		t.Fatal("scanning NULL into non-nullable *uuid.UUID should error, got nil")
	}
	if !strings.Contains(err.Error(), "NULL") {
		t.Fatalf("expected NULL-related error, got %v", err)
	}
}
