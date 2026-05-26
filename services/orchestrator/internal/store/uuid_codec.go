package store

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// registerUUIDCodec teaches a pgx TypeMap that the Postgres `uuid`
// type round-trips through google/uuid.UUID and uuid.NullUUID.
//
// pgx v5 ships pgtype.UUIDCodec which already speaks both text and
// binary on the wire, but its UUIDValuer / UUIDScanner interfaces are
// only satisfied by pgtype.UUID — not google/uuid.UUID. We bridge the
// gap with TryWrap*PlanFuncs: when pgx encounters one of the google
// types as an encode value or scan target, our wrapper converts it to
// pgtype.UUID and hands off to the built-in codec.
//
// This is the inlined equivalent of the abandoned
// github.com/vgarvardt/pgx-google-uuid/v5 package (see commits
// a6b0781 → ff91273 for why the external dep was dropped).
func registerUUIDCodec(m *pgtype.Map) {
	// Prepend so our wrappers are tried before pgx's built-in
	// pointer-deref / underlying-type wrappers — those would otherwise
	// claim `uuid.UUID` (it has underlying type [16]byte) and route it
	// through the byteaCodec, which is not what we want.
	m.TryWrapEncodePlanFuncs = append(
		[]pgtype.TryWrapEncodePlanFunc{tryWrapGoogleUUIDEncodePlan},
		m.TryWrapEncodePlanFuncs...,
	)
	m.TryWrapScanPlanFuncs = append(
		[]pgtype.TryWrapScanPlanFunc{tryWrapGoogleUUIDScanPlan},
		m.TryWrapScanPlanFuncs...,
	)

	m.RegisterType(&pgtype.Type{
		Codec: pgtype.UUIDCodec{},
		Name:  "uuid",
		OID:   pgtype.UUIDOID,
	})

	m.RegisterDefaultPgType(uuid.UUID{}, "uuid")
	m.RegisterDefaultPgType(&uuid.UUID{}, "uuid")
	m.RegisterDefaultPgType(uuid.NullUUID{}, "uuid")
	m.RegisterDefaultPgType(&uuid.NullUUID{}, "uuid")
}

// ─── wrapper types ─────────────────────────────────────────────────
//
// Both wrappers share the underlying memory layout of the google type
// they wrap, which lets us cheaply cast `*uuid.UUID` → `*googleUUID`
// without copying. SkipUnderlyingTypePlan stops pgx from re-wrapping
// us as plain [16]byte via TryWrapFindUnderlyingTypeEncodePlan.

type googleUUID uuid.UUID

func (googleUUID) SkipUnderlyingTypePlan() {}

func (w googleUUID) UUIDValue() (pgtype.UUID, error) {
	return pgtype.UUID{Bytes: [16]byte(w), Valid: true}, nil
}

func (w *googleUUID) ScanUUID(v pgtype.UUID) error {
	if !v.Valid {
		return fmt.Errorf("cannot scan NULL into *uuid.UUID")
	}
	*w = googleUUID(v.Bytes)
	return nil
}

type googleNullUUID uuid.NullUUID

func (googleNullUUID) SkipUnderlyingTypePlan() {}

func (w googleNullUUID) UUIDValue() (pgtype.UUID, error) {
	return pgtype.UUID{Bytes: [16]byte(w.UUID), Valid: w.Valid}, nil
}

func (w *googleNullUUID) ScanUUID(v pgtype.UUID) error {
	*w = googleNullUUID{UUID: uuid.UUID(v.Bytes), Valid: v.Valid}
	return nil
}

// ─── encode plan ───────────────────────────────────────────────────

func tryWrapGoogleUUIDEncodePlan(value any) (pgtype.WrappedEncodePlanNextSetter, any, bool) {
	switch v := value.(type) {
	case uuid.UUID:
		return &wrapGoogleUUIDEncodePlan{}, googleUUID(v), true
	case uuid.NullUUID:
		return &wrapGoogleNullUUIDEncodePlan{}, googleNullUUID(v), true
	}
	return nil, nil, false
}

type wrapGoogleUUIDEncodePlan struct{ next pgtype.EncodePlan }

func (p *wrapGoogleUUIDEncodePlan) SetNext(next pgtype.EncodePlan) { p.next = next }
func (p *wrapGoogleUUIDEncodePlan) Encode(value any, buf []byte) ([]byte, error) {
	return p.next.Encode(googleUUID(value.(uuid.UUID)), buf)
}

type wrapGoogleNullUUIDEncodePlan struct{ next pgtype.EncodePlan }

func (p *wrapGoogleNullUUIDEncodePlan) SetNext(next pgtype.EncodePlan) { p.next = next }
func (p *wrapGoogleNullUUIDEncodePlan) Encode(value any, buf []byte) ([]byte, error) {
	return p.next.Encode(googleNullUUID(value.(uuid.NullUUID)), buf)
}

// ─── scan plan ─────────────────────────────────────────────────────

func tryWrapGoogleUUIDScanPlan(target any) (pgtype.WrappedScanPlanNextSetter, any, bool) {
	switch t := target.(type) {
	case *uuid.UUID:
		return &wrapGoogleUUIDScanPlan{}, (*googleUUID)(t), true
	case *uuid.NullUUID:
		return &wrapGoogleNullUUIDScanPlan{}, (*googleNullUUID)(t), true
	}
	return nil, nil, false
}

type wrapGoogleUUIDScanPlan struct{ next pgtype.ScanPlan }

func (p *wrapGoogleUUIDScanPlan) SetNext(next pgtype.ScanPlan) { p.next = next }
func (p *wrapGoogleUUIDScanPlan) Scan(src []byte, dst any) error {
	return p.next.Scan(src, (*googleUUID)(dst.(*uuid.UUID)))
}

type wrapGoogleNullUUIDScanPlan struct{ next pgtype.ScanPlan }

func (p *wrapGoogleNullUUIDScanPlan) SetNext(next pgtype.ScanPlan) { p.next = next }
func (p *wrapGoogleNullUUIDScanPlan) Scan(src []byte, dst any) error {
	return p.next.Scan(src, (*googleNullUUID)(dst.(*uuid.NullUUID)))
}
