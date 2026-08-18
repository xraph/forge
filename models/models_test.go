package models

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/grove/hook"
)

// Grove calls these through interface assertions at query time, not through a
// compile-time contract, so a signature that drifts would fail silently at run
// time by simply never being called. These assertions turn that into a build
// error instead.
var (
	_ hook.BeforeInsertHook = (*BaseModel)(nil)
	_ hook.BeforeUpdateHook = (*BaseModel)(nil)
	_ hook.BeforeInsertHook = (*UUIDModel)(nil)
	_ hook.BeforeUpdateHook = (*UUIDModel)(nil)
	_ hook.BeforeInsertHook = (*XIDModel)(nil)
	_ hook.BeforeUpdateHook = (*XIDModel)(nil)
	_ hook.BeforeInsertHook = (*SoftDeleteModel)(nil)
	_ hook.BeforeUpdateHook = (*SoftDeleteModel)(nil)
	_ hook.BeforeDeleteHook = (*SoftDeleteModel)(nil)
	_ hook.BeforeInsertHook = (*UUIDSoftDeleteModel)(nil)
	_ hook.BeforeDeleteHook = (*UUIDSoftDeleteModel)(nil)
	_ hook.BeforeInsertHook = (*XIDSoftDeleteModel)(nil)
	_ hook.BeforeDeleteHook = (*XIDSoftDeleteModel)(nil)
	_ hook.BeforeInsertHook = (*TimestampModel)(nil)
	_ hook.BeforeUpdateHook = (*TimestampModel)(nil)
	_ hook.BeforeInsertHook = (*AuditModel)(nil)
	_ hook.BeforeUpdateHook = (*AuditModel)(nil)
	_ hook.BeforeDeleteHook = (*AuditModel)(nil)
	_ hook.BeforeInsertHook = (*XIDAuditModel)(nil)
	_ hook.BeforeUpdateHook = (*XIDAuditModel)(nil)
	_ hook.BeforeDeleteHook = (*XIDAuditModel)(nil)
)

func TestBaseModelStampsTimestamps(t *testing.T) {
	var m BaseModel

	if err := m.BeforeInsert(context.Background(), nil); err != nil {
		t.Fatalf("before insert: %v", err)
	}

	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		t.Fatal("insert left a timestamp unset")
	}

	first := m.UpdatedAt
	time.Sleep(time.Millisecond)

	if err := m.BeforeUpdate(context.Background(), nil); err != nil {
		t.Fatalf("before update: %v", err)
	}

	if !m.UpdatedAt.After(first) {
		t.Error("update did not move UpdatedAt")
	}
}

// An insert must not overwrite a CreatedAt the caller set deliberately.
func TestBaseModelKeepsAnExplicitCreatedAt(t *testing.T) {
	want := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	m := BaseModel{CreatedAt: want}

	if err := m.BeforeInsert(context.Background(), nil); err != nil {
		t.Fatalf("before insert: %v", err)
	}

	if !m.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt was overwritten: got %v, want %v", m.CreatedAt, want)
	}
}

func TestSoftDeleteRoundTrip(t *testing.T) {
	var m SoftDeleteModel

	if m.IsDeleted() {
		t.Fatal("a fresh model reports itself deleted")
	}

	if err := m.BeforeDelete(context.Background(), nil); err != nil {
		t.Fatalf("before delete: %v", err)
	}

	if !m.IsDeleted() {
		t.Error("delete did not mark the model deleted")
	}

	m.Restore()

	if m.IsDeleted() {
		t.Error("restore did not clear the deletion")
	}
}

func TestUUIDAndXIDModelsGenerateIdentifiers(t *testing.T) {
	var u UUIDModel
	if err := u.BeforeInsert(context.Background(), nil); err != nil {
		t.Fatalf("uuid before insert: %v", err)
	}

	if u.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Error("UUIDModel did not generate an id")
	}

	var x XIDModel
	if err := x.BeforeInsert(context.Background(), nil); err != nil {
		t.Fatalf("xid before insert: %v", err)
	}

	if x.ID.IsNil() {
		t.Error("XIDModel did not generate an id")
	}
}

func TestAuditModelRecordsTheActor(t *testing.T) {
	ctx := SetUserID(context.Background(), 42)

	var m AuditModel
	if err := m.BeforeInsert(ctx, nil); err != nil {
		t.Fatalf("before insert: %v", err)
	}

	if m.CreatedBy == nil || *m.CreatedBy != 42 {
		t.Errorf("CreatedBy = %v, want 42", m.CreatedBy)
	}
}
