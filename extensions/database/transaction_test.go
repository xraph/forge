package database

import (
	"context"
	"errors"
	"testing"
)

func TestWithTransaction(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	err := WithTransaction(ctx, db, func(txCtx context.Context) error {
		// Create user in transaction
		user := &TestUser{Name: "Test", Email: "test@example.com"}
		_, err := db.NewInsert().Model(user).Exec(txCtx)

		return err
	})

	AssertNoDatabaseError(t, err)

	// Verify user was created
	var count int

	count, err = db.NewSelect().Model((*TestUser)(nil)).Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}

func TestWithTransactionRollback(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	testErr := errors.New("test error")

	err := WithTransaction(ctx, db, func(txCtx context.Context) error {
		// Create user in transaction - use the transaction context's DB
		txDB := GetDB(txCtx, db)
		user := &TestUser{Name: "Test", Email: "test@example.com"}

		_, err := txDB.NewInsert().Model(user).Exec(txCtx)
		if err != nil {
			return err
		}

		// Return error to trigger rollback
		return testErr
	})

	if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}

	// Verify user was NOT created (rollback)
	var count int

	count, err = db.NewSelect().Model((*TestUser)(nil)).Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 0 {
		t.Errorf("expected 0 users after rollback, got %d", count)
	}
}

func TestNestedTransaction(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	err := WithTransaction(ctx, db, func(ctx1 context.Context) error {
		// Create user in outer transaction
		user1 := &TestUser{Name: "User1", Email: "user1@example.com"}

		_, err := db.NewInsert().Model(user1).Exec(ctx1)
		if err != nil {
			return err
		}

		// Nested transaction with savepoint
		return WithNestedTransaction(ctx1, func(ctx2 context.Context) error {
			// Create user in inner transaction
			user2 := &TestUser{Name: "User2", Email: "user2@example.com"}
			_, err := db.NewInsert().Model(user2).Exec(ctx2)

			return err
		})
	})

	AssertNoDatabaseError(t, err)

	// Verify both users were created
	var count int

	count, err = db.NewSelect().Model((*TestUser)(nil)).Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}
}

func TestNestedTransactionRollback(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	testErr := errors.New("inner transaction error")

	err := WithTransaction(ctx, db, func(ctx1 context.Context) error {
		// Create user in outer transaction - use transaction DB
		txDB := GetDB(ctx1, db)
		user1 := &TestUser{Name: "User1", Email: "user1@example.com"}

		_, err := txDB.NewInsert().Model(user1).Exec(ctx1)
		if err != nil {
			return err
		}

		// Nested transaction that fails
		err = WithNestedTransaction(ctx1, func(ctx2 context.Context) error {
			// Create user in inner transaction - use transaction DB
			txDB2 := GetDB(ctx2, db)
			user2 := &TestUser{Name: "User2", Email: "user2@example.com"}

			_, err := txDB2.NewInsert().Model(user2).Exec(ctx2)
			if err != nil {
				return err
			}

			// Return error to rollback inner transaction
			return testErr
		})

		// Don't propagate the error - outer transaction should still commit
		if err != nil {
			return nil // Ignore inner transaction error
		}

		return nil
	})

	AssertNoDatabaseError(t, err)

	// Verify only User1 was created (inner transaction rolled back)
	var count int

	count, err = db.NewSelect().Model((*TestUser)(nil)).Count(ctx)
	AssertNoDatabaseError(t, err)

	if count != 1 {
		t.Errorf("expected 1 user (inner rolled back), got %d", count)
	}
}

func TestIsInTransaction(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Outside transaction
	if IsInTransaction(ctx) {
		t.Error("expected IsInTransaction to be false outside transaction")
	}

	// Inside transaction
	err := WithTransaction(ctx, db, func(txCtx context.Context) error {
		if !IsInTransaction(txCtx) {
			t.Error("expected IsInTransaction to be true inside transaction")
		}

		return nil
	})

	AssertNoDatabaseError(t, err)
}

func TestGetDB(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Outside transaction - should return default DB
	dbConn := GetDB(ctx, db)
	if dbConn == nil {
		t.Fatal("expected GetDB to return default DB")
	}

	// Inside transaction - should return transaction
	err := WithTransaction(ctx, db, func(txCtx context.Context) error {
		txConn := GetDB(txCtx, db)
		if txConn == nil {
			t.Fatal("expected GetDB to return transaction")
		}

		// Should be different from default DB
		if txConn == db {
			t.Error("expected GetDB to return transaction, not default DB")
		}

		return nil
	})

	AssertNoDatabaseError(t, err)
}

func TestTransactionPanicRecovery(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	err := WithTransaction(ctx, db, func(txCtx context.Context) error {
		// This should panic and be recovered
		panic("test panic")
	})
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}

	if err.Error() != "panic in transaction: test panic" {
		t.Errorf("expected panic recovery error, got: %v", err)
	}
}
