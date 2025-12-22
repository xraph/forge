package database

import (
	"context"
	"testing"
)

// Test model
type TestUser struct {
	ID    int64  `bun:"id,pk,autoincrement" json:"id"`
	Name  string `bun:"name,notnull" json:"name"`
	Email string `bun:"email,notnull,unique" json:"email"`
	Age   int    `bun:"age" json:"age"`
}

func TestRepository_Create(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	user := &TestUser{
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   30,
	}

	err := repo.Create(ctx, user)
	AssertNoDatabaseError(t, err)

	if user.ID == 0 {
		t.Fatal("expected ID to be set after create")
	}
}

func TestRepository_FindByID(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	// Create a user
	user := &TestUser{Name: "Jane Doe", Email: "jane@example.com", Age: 25}
	err := repo.Create(ctx, user)
	AssertNoDatabaseError(t, err)

	// Find by ID
	found, err := repo.FindByID(ctx, user.ID)
	AssertNoDatabaseError(t, err)

	if found.Name != user.Name {
		t.Errorf("expected name %s, got %s", user.Name, found.Name)
	}
}

func TestRepository_FindAll(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	// Create multiple users
	users := []TestUser{
		{Name: "User1", Email: "user1@example.com", Age: 20},
		{Name: "User2", Email: "user2@example.com", Age: 30},
		{Name: "User3", Email: "user3@example.com", Age: 40},
	}

	for i := range users {
		err := repo.Create(ctx, &users[i])
		AssertNoDatabaseError(t, err)
	}

	// Find all
	all, err := repo.FindAll(ctx)
	AssertNoDatabaseError(t, err)

	if len(all) != 3 {
		t.Errorf("expected 3 users, got %d", len(all))
	}
}

func TestRepository_Update(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	// Create user
	user := &TestUser{Name: "Original Name", Email: "test@example.com", Age: 25}
	err := repo.Create(ctx, user)
	AssertNoDatabaseError(t, err)

	// Update
	user.Name = "Updated Name"
	err = repo.Update(ctx, user)
	AssertNoDatabaseError(t, err)

	// Verify update
	found, err := repo.FindByID(ctx, user.ID)
	AssertNoDatabaseError(t, err)

	if found.Name != "Updated Name" {
		t.Errorf("expected name to be updated to 'Updated Name', got %s", found.Name)
	}
}

func TestRepository_Delete(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	// Create user
	user := &TestUser{Name: "To Delete", Email: "delete@example.com", Age: 25}
	err := repo.Create(ctx, user)
	AssertNoDatabaseError(t, err)

	// Delete
	err = repo.Delete(ctx, user.ID)
	AssertNoDatabaseError(t, err)

	// Verify deletion
	_, err = repo.FindByID(ctx, user.ID)
	AssertDatabaseError(t, err) // Should return error (not found)
}

func TestRepository_Count(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	// Create users
	for i := 0; i < 5; i++ {
		user := &TestUser{Name: "User", Email: "user" + string(rune(i)) + "@example.com"}
		err := repo.Create(ctx, user)
		AssertNoDatabaseError(t, err)
	}

	// Count
	count, err := repo.Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 5 {
		t.Errorf("expected count 5, got %d", count)
	}
}

func TestRepository_Exists(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	// Create user
	user := &TestUser{Name: "Test", Email: "test@example.com"}
	err := repo.Create(ctx, user)
	AssertNoDatabaseError(t, err)

	// Check existence
	exists, err := repo.Exists(ctx, user.ID)
	AssertNoDatabaseError(t, err)

	if !exists {
		t.Error("expected user to exist")
	}

	// Check non-existence
	exists, err = repo.Exists(ctx, 99999)
	AssertNoDatabaseError(t, err)

	if exists {
		t.Error("expected user to not exist")
	}
}

func TestRepository_CreateMany(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	users := []TestUser{
		{Name: "User1", Email: "user1@example.com"},
		{Name: "User2", Email: "user2@example.com"},
		{Name: "User3", Email: "user3@example.com"},
	}

	err := repo.CreateMany(ctx, users)
	AssertNoDatabaseError(t, err)

	count, err := repo.Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 3 {
		t.Errorf("expected 3 users, got %d", count)
	}
}

func TestRepository_QueryOptions(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	repo := NewRepository[TestUser](db)
	ctx := context.Background()

	// Create users with different ages
	users := []TestUser{
		{Name: "Young", Email: "young@example.com", Age: 20},
		{Name: "Middle", Email: "middle@example.com", Age: 30},
		{Name: "Old", Email: "old@example.com", Age: 40},
	}

	for i := range users {
		err := repo.Create(ctx, &users[i])
		AssertNoDatabaseError(t, err)
	}

	// Test WithLimit
	limited, err := repo.FindAll(ctx, WithLimit(2))
	AssertNoDatabaseError(t, err)

	if len(limited) != 2 {
		t.Errorf("expected 2 users with limit, got %d", len(limited))
	}

	// Test WithOrder
	ordered, err := repo.FindAll(ctx, WithOrder("age", "desc"))
	AssertNoDatabaseError(t, err)

	if len(ordered) > 0 && ordered[0].Age != 40 {
		t.Errorf("expected first user to have age 40, got %d", ordered[0].Age)
	}
}
