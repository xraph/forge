package forge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/vessel"
)

// fakeCentralMigrator is a recording fake that satisfies the CentralMigrator interface.
type fakeCentralMigrator struct {
	runAllCalled      bool
	rollbackAllCalled bool
	statusAllCalled   bool

	runAllResult      *MigrationResult
	rollbackAllResult *MigrationResult
	statusAllResult   []*MigrationGroupInfo
}

func newFakeCentralMigrator() *fakeCentralMigrator {
	return &fakeCentralMigrator{
		runAllResult:      &MigrationResult{Applied: 2, Names: []string{"001_init", "002_users"}},
		rollbackAllResult: &MigrationResult{RolledBack: 1, Names: []string{"002_users"}},
		statusAllResult: []*MigrationGroupInfo{
			{
				Name: "core",
				Applied: []*MigrationInfo{
					{Version: "001", Name: "init", AppliedAt: "2024-01-01T00:00:00Z"},
				},
				Pending: []*MigrationInfo{
					{Version: "002", Name: "users"},
				},
			},
		},
	}
}

func (f *fakeCentralMigrator) RunAll(_ context.Context) (*MigrationResult, error) {
	f.runAllCalled = true
	return f.runAllResult, nil
}

func (f *fakeCentralMigrator) RollbackAll(_ context.Context) (*MigrationResult, error) {
	f.rollbackAllCalled = true
	return f.rollbackAllResult, nil
}

func (f *fakeCentralMigrator) StatusAll(_ context.Context) ([]*MigrationGroupInfo, error) {
	f.statusAllCalled = true
	return f.statusAllResult, nil
}

// Compile-time assertion: fakeCentralMigrator satisfies CentralMigrator.
var _ CentralMigrator = (*fakeCentralMigrator)(nil)

// TestApp_CentralMigratorResolves verifies that CentralMigrator() returns the
// implementation when one is registered in the container, and ok=false otherwise.
func TestApp_CentralMigratorResolves(t *testing.T) {
	t.Run("ReturnsFalseWhenNothingRegistered", func(t *testing.T) {
		testLogger := logger.NewTestLogger()
		a := NewApp(AppConfig{Logger: testLogger})
		cm, ok := a.CentralMigrator()
		assert.False(t, ok, "CentralMigrator() should return false when nothing is registered")
		assert.Nil(t, cm)
	})

	t.Run("ReturnsTrueWhenRegistered", func(t *testing.T) {
		testLogger := logger.NewTestLogger()
		a := NewApp(AppConfig{Logger: testLogger, CentralMigrations: true})

		fake := newFakeCentralMigrator()
		err := vessel.ProvideValue[CentralMigrator](a.Container(), fake)
		require.NoError(t, err, "ProvideValue should succeed")

		cm, ok := a.CentralMigrator()
		assert.True(t, ok, "CentralMigrator() should return true when registered")
		assert.NotNil(t, cm)

		// Verify it's the same instance we registered.
		result, err := cm.RunAll(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 2, result.Applied)
		assert.True(t, fake.runAllCalled)
	})
}

// TestApp_SetMigrationsDisabled verifies the runtime setter toggles the config flag.
func TestApp_SetMigrationsDisabled(t *testing.T) {
	testLogger := logger.NewTestLogger()
	a := NewApp(AppConfig{Logger: testLogger})

	assert.False(t, a.MigrationsDisabled(), "should be false by default")

	a.SetMigrationsDisabled(true)
	assert.True(t, a.MigrationsDisabled(), "should be true after SetMigrationsDisabled(true)")

	a.SetMigrationsDisabled(false)
	assert.False(t, a.MigrationsDisabled(), "should be false after SetMigrationsDisabled(false)")
}

// TestApp_SetMigrationsDisabledViaConfig verifies the initial config flag is respected.
func TestApp_SetMigrationsDisabledViaConfig(t *testing.T) {
	testLogger := logger.NewTestLogger()
	a := NewApp(AppConfig{Logger: testLogger, DisableMigrations: true})
	assert.True(t, a.MigrationsDisabled())

	// Override at runtime.
	a.SetMigrationsDisabled(false)
	assert.False(t, a.MigrationsDisabled())
}

// TestApp_CentralMigratorFakeCallsThrough verifies each method on the fake
// records the call and returns the expected canned result.
func TestApp_CentralMigratorFakeCallsThrough(t *testing.T) {
	testLogger := logger.NewTestLogger()
	a := NewApp(AppConfig{Logger: testLogger})

	fake := newFakeCentralMigrator()
	require.NoError(t, vessel.ProvideValue[CentralMigrator](a.Container(), fake))

	cm, ok := a.CentralMigrator()
	require.True(t, ok)

	ctx := context.Background()

	// RunAll
	r, err := cm.RunAll(ctx)
	require.NoError(t, err)
	assert.True(t, fake.runAllCalled)
	assert.Equal(t, 2, r.Applied)
	assert.Equal(t, []string{"001_init", "002_users"}, r.Names)

	// RollbackAll
	rb, err := cm.RollbackAll(ctx)
	require.NoError(t, err)
	assert.True(t, fake.rollbackAllCalled)
	assert.Equal(t, 1, rb.RolledBack)
	assert.Equal(t, []string{"002_users"}, rb.Names)

	// StatusAll
	groups, err := cm.StatusAll(ctx)
	require.NoError(t, err)
	assert.True(t, fake.statusAllCalled)
	require.Len(t, groups, 1)
	assert.Equal(t, "core", groups[0].Name)
	require.Len(t, groups[0].Applied, 1)
	require.Len(t, groups[0].Pending, 1)
}
