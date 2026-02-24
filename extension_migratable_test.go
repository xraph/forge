package forge

import (
	"context"
	"testing"
)

// mockMigratableExtension is a test double that implements both Extension and MigratableExtension.
type mockMigratableExtension struct {
	name           string
	migrateResult  *MigrationResult
	migrateErr     error
	rollbackResult *MigrationResult
	rollbackErr    error
	statusResult   []*MigrationGroupInfo
	statusErr      error
}

func (m *mockMigratableExtension) Name() string                   { return m.name }
func (m *mockMigratableExtension) Version() string                { return "1.0.0" }
func (m *mockMigratableExtension) Description() string            { return "mock migratable" }
func (m *mockMigratableExtension) Register(_ App) error           { return nil }
func (m *mockMigratableExtension) Start(_ context.Context) error  { return nil }
func (m *mockMigratableExtension) Stop(_ context.Context) error   { return nil }
func (m *mockMigratableExtension) Health(_ context.Context) error { return nil }
func (m *mockMigratableExtension) Dependencies() []string         { return nil }
func (m *mockMigratableExtension) Migrate(_ context.Context) (*MigrationResult, error) {
	return m.migrateResult, m.migrateErr
}
func (m *mockMigratableExtension) Rollback(_ context.Context) (*MigrationResult, error) {
	return m.rollbackResult, m.rollbackErr
}
func (m *mockMigratableExtension) MigrationStatus(_ context.Context) ([]*MigrationGroupInfo, error) {
	return m.statusResult, m.statusErr
}

func TestMigratableExtension_InterfaceCompliance(t *testing.T) {
	// Verify that mockMigratableExtension satisfies both interfaces.
	var _ Extension = (*mockMigratableExtension)(nil)
	var _ MigratableExtension = (*mockMigratableExtension)(nil)
}

func TestMigratableExtension_TypeAssertion(t *testing.T) {
	ext := &mockMigratableExtension{name: "test-ext"}

	// MigratableExtension embeds Extension, so type assertion should work both ways.
	var e Extension = ext
	m, ok := e.(MigratableExtension)
	if !ok {
		t.Fatal("expected Extension to be assertable to MigratableExtension")
	}

	if m.Name() != "test-ext" {
		t.Errorf("expected name 'test-ext', got '%s'", m.Name())
	}
}

func TestMigratableExtension_NonMigratableTypeAssertion(t *testing.T) {
	// A plain extension that does NOT implement MigratableExtension.
	ext := &plainExtension{name: "plain"}

	var e Extension = ext
	_, ok := e.(MigratableExtension)
	if ok {
		t.Fatal("expected plain Extension to NOT be assertable to MigratableExtension")
	}
}

func TestMigrationResult_ZeroValue(t *testing.T) {
	result := &MigrationResult{}
	if result.Applied != 0 {
		t.Errorf("expected Applied to be 0, got %d", result.Applied)
	}
	if result.RolledBack != 0 {
		t.Errorf("expected RolledBack to be 0, got %d", result.RolledBack)
	}
	if result.Names != nil {
		t.Errorf("expected Names to be nil, got %v", result.Names)
	}
}

func TestMigrationGroupInfo_Structure(t *testing.T) {
	info := &MigrationGroupInfo{
		Name: "core",
		Applied: []*MigrationInfo{
			{Name: "create_users", Version: "20240101120000", Group: "core", Applied: true, AppliedAt: "2024-01-01T12:00:00Z"},
		},
		Pending: []*MigrationInfo{
			{Name: "add_email_index", Version: "20240201120000", Group: "core", Applied: false},
		},
	}

	if info.Name != "core" {
		t.Errorf("expected name 'core', got '%s'", info.Name)
	}
	if len(info.Applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(info.Applied))
	}
	if len(info.Pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(info.Pending))
	}
	if info.Applied[0].Name != "create_users" {
		t.Errorf("expected applied name 'create_users', got '%s'", info.Applied[0].Name)
	}
	if info.Applied[0].Applied != true {
		t.Error("expected Applied to be true")
	}
	if info.Pending[0].AppliedAt != "" {
		t.Errorf("expected empty AppliedAt for pending, got '%s'", info.Pending[0].AppliedAt)
	}
}

// plainExtension is a minimal extension that does NOT implement MigratableExtension.
type plainExtension struct {
	name string
}

func (p *plainExtension) Name() string                   { return p.name }
func (p *plainExtension) Version() string                { return "1.0.0" }
func (p *plainExtension) Description() string            { return "plain" }
func (p *plainExtension) Register(_ App) error           { return nil }
func (p *plainExtension) Start(_ context.Context) error  { return nil }
func (p *plainExtension) Stop(_ context.Context) error   { return nil }
func (p *plainExtension) Health(_ context.Context) error { return nil }
func (p *plainExtension) Dependencies() []string         { return nil }
