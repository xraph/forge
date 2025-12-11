package database

import (
	"context"

	"github.com/uptrace/bun"
)

// ExampleSeeder demonstrates how to create a seeder.
// This can be used as a template for your own seeders.
type ExampleSeeder struct {
	// You can add dependencies here (e.g., config, services)
}

// Name returns the unique identifier for this seeder.
func (s *ExampleSeeder) Name() string {
	return "example_seeder"
}

// Seed executes the seeding logic.
func (s *ExampleSeeder) Seed(ctx context.Context, db *bun.DB) error {
	// Example 1: Simple insert with idempotency
	// users := []User{
	// 	{ID: 1, Email: "admin@example.com", Name: "Admin User"},
	// 	{ID: 2, Email: "user@example.com", Name: "Regular User"},
	// }
	//
	// // Use IdempotentInsert to ensure this can run multiple times
	// if err := IdempotentInsert(ctx, db, &users, "email"); err != nil {
	// 	return fmt.Errorf("failed to seed users: %w", err)
	// }

	// Example 2: Conditional seeding
	// var count int
	// count, err := db.NewSelect().Model((*User)(nil)).Count(ctx)
	// if err != nil {
	// 	return fmt.Errorf("failed to count users: %w", err)
	// }
	//
	// if count == 0 {
	// 	// Only seed if table is empty
	// 	users := generateUsers(100)
	// 	if err := BulkInsert(ctx, db, users, 0); err != nil {
	// 		return fmt.Errorf("failed to bulk insert users: %w", err)
	// 	}
	// }

	// Example 3: Seeding with relationships
	// user := User{ID: 1, Email: "admin@example.com", Name: "Admin"}
	// if err := IdempotentInsert(ctx, db, &[]User{user}, "email"); err != nil {
	// 	return err
	// }
	//
	// posts := []Post{
	// 	{UserID: 1, Title: "First Post", Content: "Hello World"},
	// 	{UserID: 1, Title: "Second Post", Content: "Another post"},
	// }
	// if err := IdempotentInsert(ctx, db, &posts, "title"); err != nil {
	// 	return err
	// }

	// Example 4: Using transactions for complex seeding
	// return WithTransaction(ctx, db, func(txCtx context.Context) error {
	// 	// All operations in this block are atomic
	// 	if err := seedUsers(txCtx, db); err != nil {
	// 		return err
	// 	}
	// 	if err := seedPosts(txCtx, db); err != nil {
	// 		return err
	// 	}
	// 	return nil
	// })

	// For this example, we don't actually seed anything
	return nil
}

// Here are some example helper functions you might use in your seeders:

// Example of generating test data
// func generateUsers(count int) []User {
// 	users := make([]User, count)
// 	for i := 0; i < count; i++ {
// 		users[i] = User{
// 			Email: fmt.Sprintf("user%d@example.com", i+1),
// 			Name:  fmt.Sprintf("User %d", i+1),
// 		}
// 	}
// 	return users
// }

// Example of seeding with deterministic IDs using XID
// func seedUsersWithXID(ctx context.Context, db *bun.DB) error {
// 	// Using a fixed seed for deterministic IDs
// 	users := []User{
// 		{ID: xid.ID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, Name: "Admin"},
// 		{ID: xid.ID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}, Name: "User"},
// 	}
// 	return IdempotentInsert(ctx, db, &users, "id")
// }

// Example of seeding from external data source
// func seedFromJSON(ctx context.Context, db *bun.DB, filename string) error {
// 	data, err := os.ReadFile(filename)
// 	if err != nil {
// 		return err
// 	}
//
// 	var users []User
// 	if err := json.Unmarshal(data, &users); err != nil {
// 		return err
// 	}
//
// 	return BulkInsert(ctx, db, users, 0)
// }

// Example of using SeederFunc for inline seeders
// func CreateUserSeeder() Seeder {
// 	return NewSeederFunc("users", func(ctx context.Context, db *bun.DB) error {
// 		users := []User{
// 			{Email: "admin@example.com", Name: "Admin"},
// 			{Email: "user@example.com", Name: "User"},
// 		}
// 		return IdempotentInsert(ctx, db, &users, "email")
// 	})
// }

