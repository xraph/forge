package database

import (
	"context"
	"testing"
)

func TestBulkInsert(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Create 100 users
	users := make([]TestUser, 100)
	for i := 0; i < 100; i++ {
		users[i] = TestUser{
			Name:  "User" + string(rune(i)),
			Email: "user" + string(rune(i)) + "@example.com",
			Age:   20 + i,
		}
	}

	err := BulkInsert(ctx, db, users, 50) // Batch size 50
	AssertNoDatabaseError(t, err)

	// Verify all users were inserted
	count, err := db.NewSelect().Model((*TestUser)(nil)).Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 100 {
		t.Errorf("expected 100 users, got %d", count)
	}
}

func TestBulkInsertEmpty(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Empty slice should not error
	err := BulkInsert(ctx, db, []TestUser{}, 10)
	AssertNoDatabaseError(t, err)
}

func TestBulkUpdate(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Create users
	users := make([]TestUser, 10)
	for i := 0; i < 10; i++ {
		users[i] = TestUser{
			Name:  "User" + string(rune(i)),
			Email: "user" + string(rune(i)) + "@example.com",
			Age:   20,
		}
	}
	err := BulkInsert(ctx, db, users, 0)
	AssertNoDatabaseError(t, err)

	// Fetch users to get IDs
	var fetchedUsers []TestUser
	err = db.NewSelect().Model(&fetchedUsers).Scan(ctx)
	AssertNoDatabaseError(t, err)

	// Update ages
	for i := range fetchedUsers {
		fetchedUsers[i].Age = 30
	}

	err = BulkUpdate(ctx, db, fetchedUsers, []string{"age"}, 0)
	AssertNoDatabaseError(t, err)

	// Verify updates
	var updated []TestUser
	err = db.NewSelect().Model(&updated).Scan(ctx)
	AssertNoDatabaseError(t, err)

	for _, u := range updated {
		if u.Age != 30 {
			t.Errorf("expected age 30, got %d", u.Age)
		}
	}
}

func TestBulkDelete(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Create users
	users := make([]TestUser, 10)
	for i := 0; i < 10; i++ {
		users[i] = TestUser{
			Name:  "User" + string(rune(i)),
			Email: "user" + string(rune(i)) + "@example.com",
		}
	}
	err := BulkInsert(ctx, db, users, 0)
	AssertNoDatabaseError(t, err)

	// Get IDs
	var fetchedUsers []TestUser
	err = db.NewSelect().Model(&fetchedUsers).Scan(ctx)
	AssertNoDatabaseError(t, err)

	// Delete first 5
	ids := make([]any, 5)
	for i := 0; i < 5; i++ {
		ids[i] = fetchedUsers[i].ID
	}

	err = BulkDelete[TestUser](ctx, db, ids)
	AssertNoDatabaseError(t, err)

	// Verify deletion
	count, err := db.NewSelect().Model((*TestUser)(nil)).Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 5 {
		t.Errorf("expected 5 users remaining, got %d", count)
	}
}

func TestChunkSlice(t *testing.T) {
	items := make([]int, 25)
	for i := 0; i < 25; i++ {
		items[i] = i
	}

	chunks := ChunkSlice(items, 10)

	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}

	if len(chunks[0]) != 10 {
		t.Errorf("expected first chunk size 10, got %d", len(chunks[0]))
	}

	if len(chunks[1]) != 10 {
		t.Errorf("expected second chunk size 10, got %d", len(chunks[1]))
	}

	if len(chunks[2]) != 5 {
		t.Errorf("expected third chunk size 5, got %d", len(chunks[2]))
	}
}
