package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/xid"
	"github.com/uptrace/bun"
)

// BaseModel provides common fields and hooks for all models
// Use this for models that need ID, timestamps, and standard hooks.
type BaseModel struct {
	ID        int64     `bun:"id,pk,autoincrement"                                   json:"id"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// BeforeInsert hook - sets timestamps on insert.
func (m *BaseModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt on every update.
func (m *BaseModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	return nil
}

// UUIDModel provides UUID-based primary key with timestamps
// Use this for distributed systems or when you need globally unique IDs.
type UUIDModel struct {
	ID        uuid.UUID `bun:"id,pk,type:uuid,default:gen_random_uuid()"             json:"id"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// BeforeInsert hook - generates UUID and sets timestamps.
func (m *UUIDModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()

	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt.
func (m *UUIDModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	return nil
}

// SoftDeleteModel provides soft delete functionality with timestamps
// Soft-deleted records are not permanently removed, just marked as deleted.
type SoftDeleteModel struct {
	ID        int64      `bun:"id,pk,autoincrement"                                   json:"id"`
	CreatedAt time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt *time.Time `bun:"deleted_at,soft_delete,nullzero"                       json:"deleted_at,omitempty"`
}

// BeforeInsert hook - sets timestamps.
func (m *SoftDeleteModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt.
func (m *SoftDeleteModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	return nil
}

// BeforeDelete hook - performs soft delete.
func (m *SoftDeleteModel) BeforeDelete(ctx context.Context, query *bun.DeleteQuery) error {
	now := time.Now()
	m.DeletedAt = &now

	return nil
}

// IsDeleted checks if the record is soft deleted.
func (m *SoftDeleteModel) IsDeleted() bool {
	return m.DeletedAt != nil
}

// Restore restores a soft-deleted record.
func (m *SoftDeleteModel) Restore() {
	m.DeletedAt = nil
}

// UUIDSoftDeleteModel combines UUID primary key with soft delete.
type UUIDSoftDeleteModel struct {
	ID        uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()"             json:"id"`
	CreatedAt time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt *time.Time `bun:"deleted_at,soft_delete,nullzero"                       json:"deleted_at,omitempty"`
}

// BeforeInsert hook - generates UUID and sets timestamps.
func (m *UUIDSoftDeleteModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()

	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt.
func (m *UUIDSoftDeleteModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	return nil
}

// BeforeDelete hook - performs soft delete.
func (m *UUIDSoftDeleteModel) BeforeDelete(ctx context.Context, query *bun.DeleteQuery) error {
	now := time.Now()
	m.DeletedAt = &now

	return nil
}

// IsDeleted checks if the record is soft deleted.
func (m *UUIDSoftDeleteModel) IsDeleted() bool {
	return m.DeletedAt != nil
}

// Restore restores a soft-deleted record.
func (m *UUIDSoftDeleteModel) Restore() {
	m.DeletedAt = nil
}

// XIDModel provides XID primary key with timestamps
// XID is a globally unique, sortable, compact, URL-safe identifier
// It's shorter than UUID (20 bytes vs 36) and sortable by creation time.
type XIDModel struct {
	ID        xid.ID    `bun:"id,pk,type:varchar(20)"                                json:"id"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// BeforeInsert hook - generates XID and sets timestamps.
func (m *XIDModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()

	if m.ID.IsNil() {
		m.ID = xid.New()
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt.
func (m *XIDModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	return nil
}

// XIDSoftDeleteModel combines XID primary key with soft delete.
type XIDSoftDeleteModel struct {
	ID        xid.ID     `bun:"id,pk,type:varchar(20)"                                json:"id"`
	CreatedAt time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt *time.Time `bun:"deleted_at,soft_delete,nullzero"                       json:"deleted_at,omitempty"`
}

// BeforeInsert hook - generates XID and sets timestamps.
func (m *XIDSoftDeleteModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()

	if m.ID.IsNil() {
		m.ID = xid.New()
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt.
func (m *XIDSoftDeleteModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	return nil
}

// BeforeDelete hook - performs soft delete.
func (m *XIDSoftDeleteModel) BeforeDelete(ctx context.Context, query *bun.DeleteQuery) error {
	now := time.Now()
	m.DeletedAt = &now

	return nil
}

// IsDeleted checks if the record is soft deleted.
func (m *XIDSoftDeleteModel) IsDeleted() bool {
	return m.DeletedAt != nil
}

// Restore restores a soft-deleted record.
func (m *XIDSoftDeleteModel) Restore() {
	m.DeletedAt = nil
}

// XIDAuditModel provides comprehensive audit trail with XID primary key and user tracking
// Combines XID with full audit capabilities: tracks who created/updated/deleted records.
type XIDAuditModel struct {
	ID        xid.ID     `bun:"id,pk,type:varchar(20)"                                json:"id"`
	CreatedAt time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	CreatedBy *xid.ID    `bun:"created_by,type:varchar(20)"                           json:"created_by,omitempty"`
	UpdatedAt time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	UpdatedBy *xid.ID    `bun:"updated_by,type:varchar(20)"                           json:"updated_by,omitempty"`
	DeletedAt *time.Time `bun:"deleted_at,soft_delete,nullzero"                       json:"deleted_at,omitempty"`
	DeletedBy *xid.ID    `bun:"deleted_by,type:varchar(20)"                           json:"deleted_by,omitempty"`
}

// BeforeInsert hook - generates XID, sets timestamps and tracks creator.
func (m *XIDAuditModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()

	if m.ID.IsNil() {
		m.ID = xid.New()
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	// Extract user ID from context if available
	if userID := getXIDUserIDFromContext(ctx); userID != nil {
		if m.CreatedBy == nil {
			m.CreatedBy = userID
		}

		if m.UpdatedBy == nil {
			m.UpdatedBy = userID
		}
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt and tracks updater.
func (m *XIDAuditModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	// Extract user ID from context if available
	if userID := getXIDUserIDFromContext(ctx); userID != nil {
		m.UpdatedBy = userID
	}

	return nil
}

// BeforeDelete hook - performs soft delete and tracks deleter.
func (m *XIDAuditModel) BeforeDelete(ctx context.Context, query *bun.DeleteQuery) error {
	now := time.Now()
	m.DeletedAt = &now

	// Extract user ID from context if available
	if userID := getXIDUserIDFromContext(ctx); userID != nil {
		m.DeletedBy = userID
	}

	return nil
}

// IsDeleted checks if the record is soft deleted.
func (m *XIDAuditModel) IsDeleted() bool {
	return m.DeletedAt != nil
}

// Restore restores a soft-deleted record.
func (m *XIDAuditModel) Restore() {
	m.DeletedAt = nil
	m.DeletedBy = nil
}

// TimestampModel provides only timestamp fields without ID
// Use this when you want to add your own ID field type.
type TimestampModel struct {
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// BeforeInsert hook - sets timestamps.
func (m *TimestampModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt.
func (m *TimestampModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	return nil
}

// AuditModel provides comprehensive audit trail with user tracking
// Use this when you need to track who created/updated records.
type AuditModel struct {
	ID        int64      `bun:"id,pk,autoincrement"                                   json:"id"`
	CreatedAt time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	CreatedBy *int64     `bun:"created_by"                                            json:"created_by,omitempty"`
	UpdatedAt time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	UpdatedBy *int64     `bun:"updated_by"                                            json:"updated_by,omitempty"`
	DeletedAt *time.Time `bun:"deleted_at,soft_delete,nullzero"                       json:"deleted_at,omitempty"`
	DeletedBy *int64     `bun:"deleted_by"                                            json:"deleted_by,omitempty"`
}

// BeforeInsert hook - sets timestamps and tracks creator.
func (m *AuditModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}

	// Extract user ID from context if available
	if userID := getUserIDFromContext(ctx); userID != nil {
		if m.CreatedBy == nil {
			m.CreatedBy = userID
		}

		if m.UpdatedBy == nil {
			m.UpdatedBy = userID
		}
	}

	return nil
}

// BeforeUpdate hook - updates UpdatedAt and tracks updater.
func (m *AuditModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	m.UpdatedAt = time.Now()

	// Extract user ID from context if available
	if userID := getUserIDFromContext(ctx); userID != nil {
		m.UpdatedBy = userID
	}

	return nil
}

// BeforeDelete hook - performs soft delete and tracks deleter.
func (m *AuditModel) BeforeDelete(ctx context.Context, query *bun.DeleteQuery) error {
	now := time.Now()
	m.DeletedAt = &now

	// Extract user ID from context if available
	if userID := getUserIDFromContext(ctx); userID != nil {
		m.DeletedBy = userID
	}

	return nil
}

// IsDeleted checks if the record is soft deleted.
func (m *AuditModel) IsDeleted() bool {
	return m.DeletedAt != nil
}

// Restore restores a soft-deleted record.
func (m *AuditModel) Restore() {
	m.DeletedAt = nil
	m.DeletedBy = nil
}

// Helper function to extract user ID from context
// Applications should set this in their auth middleware.
func getXIDUserIDFromContext(ctx context.Context) *xid.ID {
	// Check for user ID in context with common keys
	if userID, ok := ctx.Value("user_id").(xid.ID); ok {
		return &userID
	}

	if userID, ok := ctx.Value("userID").(xid.ID); ok {
		return &userID
	}

	if userID, ok := ctx.Value("user").(xid.ID); ok {
		return &userID
	}

	return nil
}

func SetXIDUserID(ctx context.Context, userID xid.ID) context.Context {
	return context.WithValue(ctx, "user_id", userID)
}

func GetXIDUserID(ctx context.Context) (xid.ID, bool) {
	if userID := getXIDUserIDFromContext(ctx); userID != nil {
		return *userID, true
	}

	return xid.ID{}, false
}

// Helper function to extract user ID from context
// Applications should set this in their auth middleware.
func getUserIDFromContext(ctx context.Context) *int64 {
	// Check for user ID in context with common keys
	if userID, ok := ctx.Value("user_id").(int64); ok {
		return &userID
	}

	if userID, ok := ctx.Value("userID").(int64); ok {
		return &userID
	}

	if userID, ok := ctx.Value("user").(int64); ok {
		return &userID
	}

	return nil
}

// SetUserID is a helper to set user ID in context for audit tracking.
func SetUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, "user_id", userID)
}

// GetUserID retrieves user ID from context.
func GetUserID(ctx context.Context) (int64, bool) {
	if userID := getUserIDFromContext(ctx); userID != nil {
		return *userID, true
	}

	return 0, false
}
