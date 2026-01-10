package database

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/xraph/forge/errors"
)

// IDB is an interface that both *bun.DB and bun.Tx implement.
// This allows Repository to work with both direct database access and transactions.
type IDB interface {
	bun.IDB
}

// Repository provides a generic, type-safe repository pattern for database operations.
// It eliminates boilerplate CRUD code while maintaining flexibility through QueryOptions.
//
// Example usage:
//
//	type User struct {
//	    ID   int64  `bun:"id,pk,autoincrement"`
//	    Name string `bun:"name"`
//	}
//
//	repo := database.NewRepository[User](db)
//	user, err := repo.FindByID(ctx, 123)
//	users, err := repo.FindAll(ctx, WhereActive())
type Repository[T any] struct {
	db IDB
}

// QueryOption is a function that modifies a SelectQuery.
// This pattern allows flexible, composable query building.
//
// Example:
//
//	func WhereActive() QueryOption {
//	    return func(q *bun.SelectQuery) *bun.SelectQuery {
//	        return q.Where("deleted_at IS NULL")
//	    }
//	}
type QueryOption func(*bun.SelectQuery) *bun.SelectQuery

// NewRepository creates a new repository instance for type T.
// The db parameter can be either *bun.DB or bun.Tx, allowing
// repositories to work seamlessly in and out of transactions.
func NewRepository[T any](db IDB) *Repository[T] {
	return &Repository[T]{db: db}
}

// DB returns the underlying database connection.
// Useful when you need direct access to Bun's query builder.
func (r *Repository[T]) DB() IDB {
	return r.db
}

// Query returns a new SelectQuery for the repository's model type.
// This is useful when you need to build custom queries that aren't
// covered by the standard repository methods.
//
// Example:
//
//	query := repo.Query().
//	    Where("age > ?", 18).
//	    Order("created_at DESC").
//	    Limit(10)
//	var users []User
//	err := query.Scan(ctx, &users)
func (r *Repository[T]) Query() *bun.SelectQuery {
	var model T

	return r.db.NewSelect().Model(&model)
}

// FindByID retrieves a single record by its primary key.
// Returns ErrRecordNotFound if no record exists.
//
// Example:
//
//	user, err := repo.FindByID(ctx, 123)
//	if errors.Is(err, database.ErrRecordNotFound(123)) {
//	    // Handle not found
//	}
func (r *Repository[T]) FindByID(ctx context.Context, id any, opts ...QueryOption) (*T, error) {
	var entity T

	query := r.db.NewSelect().Model(&entity).Where("? = ?", bun.Ident("id"), id)

	// Apply query options
	for _, opt := range opts {
		query = opt(query)
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return &entity, nil
}

// FindOne retrieves a single record based on query options.
// Returns ErrRecordNotFound if no record exists.
//
// Example:
//
//	user, err := repo.FindOne(ctx, func(q *bun.SelectQuery) *bun.SelectQuery {
//	    return q.Where("email = ?", "user@example.com")
//	})
func (r *Repository[T]) FindOne(ctx context.Context, opts ...QueryOption) (*T, error) {
	var entity T

	query := r.db.NewSelect().Model(&entity)

	// Apply query options
	for _, opt := range opts {
		query = opt(query)
	}

	err := query.Limit(1).Scan(ctx)
	if err != nil {
		return nil, err
	}

	return &entity, nil
}

// FindAll retrieves all records matching the query options.
// Returns an empty slice if no records found (not an error).
//
// Example:
//
//	users, err := repo.FindAll(ctx,
//	    func(q *bun.SelectQuery) *bun.SelectQuery {
//	        return q.Where("age > ?", 18).Order("created_at DESC")
//	    },
//	)
func (r *Repository[T]) FindAll(ctx context.Context, opts ...QueryOption) ([]T, error) {
	var entities []T

	query := r.db.NewSelect().Model(&entities)

	// Apply query options
	for _, opt := range opts {
		query = opt(query)
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return entities, nil
}

// FindAllWithDeleted retrieves all records including soft-deleted ones.
// Only works with models that have a DeletedAt field with soft_delete tag.
//
// Example:
//
//	allUsers, err := repo.FindAllWithDeleted(ctx)
func (r *Repository[T]) FindAllWithDeleted(ctx context.Context, opts ...QueryOption) ([]T, error) {
	var entities []T

	query := r.db.NewSelect().Model(&entities).WhereAllWithDeleted()

	// Apply query options
	for _, opt := range opts {
		query = opt(query)
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return entities, nil
}

// Exists checks if a record with the given ID exists.
//
// Example:
//
//	exists, err := repo.Exists(ctx, 123)
func (r *Repository[T]) Exists(ctx context.Context, id any) (bool, error) {
	var entity T

	exists, err := r.db.NewSelect().
		Model(&entity).
		Where("? = ?", bun.Ident("id"), id).
		Exists(ctx)

	return exists, err
}

// Count returns the number of records matching the query options.
//
// Example:
//
//	count, err := repo.Count(ctx, func(q *bun.SelectQuery) *bun.SelectQuery {
//	    return q.Where("age > ?", 18)
//	})
func (r *Repository[T]) Count(ctx context.Context, opts ...QueryOption) (int, error) {
	var entity T

	query := r.db.NewSelect().Model(&entity)

	// Apply query options
	for _, opt := range opts {
		query = opt(query)
	}

	count, err := query.Count(ctx)

	return count, err
}

// Create inserts a new record into the database.
// The entity's BeforeInsert hook will be called if it implements it.
//
// Example:
//
//	user := &User{Name: "John"}
//	err := repo.Create(ctx, user)
//	// user.ID is now populated
func (r *Repository[T]) Create(ctx context.Context, entity *T) error {
	_, err := r.db.NewInsert().Model(entity).Exec(ctx)

	return err
}

// CreateMany inserts multiple records in a single query.
// Much more efficient than calling Create in a loop.
//
// Example:
//
//	users := []User{{Name: "John"}, {Name: "Jane"}}
//	err := repo.CreateMany(ctx, users)
func (r *Repository[T]) CreateMany(ctx context.Context, entities []T) error {
	if len(entities) == 0 {
		return nil
	}

	_, err := r.db.NewInsert().Model(&entities).Exec(ctx)

	return err
}

// Update updates an existing record.
// The entity's BeforeUpdate hook will be called if it implements it.
// By default, all non-zero fields are updated.
//
// Example:
//
//	user.Name = "Jane"
//	err := repo.Update(ctx, user)
func (r *Repository[T]) Update(ctx context.Context, entity *T) error {
	_, err := r.db.NewUpdate().Model(entity).WherePK().Exec(ctx)

	return err
}

// UpdateColumns updates only specific columns of a record.
// Useful when you only want to update certain fields.
//
// Example:
//
//	err := repo.UpdateColumns(ctx, user, "name", "email")
func (r *Repository[T]) UpdateColumns(ctx context.Context, entity *T, columns ...string) error {
	if len(columns) == 0 {
		return errors.New("no columns specified for update")
	}

	_, err := r.db.NewUpdate().Model(entity).Column(columns...).WherePK().Exec(ctx)

	return err
}

// Delete permanently deletes a record by ID.
// For soft deletes, use SoftDelete instead.
//
// Example:
//
//	err := repo.Delete(ctx, 123)
func (r *Repository[T]) Delete(ctx context.Context, id any) error {
	var entity T

	_, err := r.db.NewDelete().Model(&entity).Where("? = ?", bun.Ident("id"), id).Exec(ctx)

	return err
}

// DeleteMany permanently deletes multiple records by IDs.
//
// Example:
//
//	err := repo.DeleteMany(ctx, []int64{1, 2, 3})
func (r *Repository[T]) DeleteMany(ctx context.Context, ids []any) error {
	if len(ids) == 0 {
		return nil
	}

	var entity T

	_, err := r.db.NewDelete().Model(&entity).Where("? IN (?)", bun.Ident("id"), bun.In(ids)).Exec(ctx)

	return err
}

// SoftDelete marks a record as deleted by setting its DeletedAt field.
// Only works with models that have a DeletedAt field with soft_delete tag.
//
// Example:
//
//	err := repo.SoftDelete(ctx, 123)
func (r *Repository[T]) SoftDelete(ctx context.Context, id any) error {
	var entity T

	_, err := r.db.NewDelete().Model(&entity).Where("? = ?", bun.Ident("id"), id).Exec(ctx)

	return err
}

// RestoreSoftDeleted restores a soft-deleted record.
// Only works with models that have a DeletedAt field with soft_delete tag.
//
// Example:
//
//	err := repo.RestoreSoftDeleted(ctx, 123)
func (r *Repository[T]) RestoreSoftDeleted(ctx context.Context, id any) error {
	var entity T

	_, err := r.db.NewUpdate().
		Model(&entity).
		Set("deleted_at = NULL").
		Where("? = ?", bun.Ident("id"), id).
		WhereAllWithDeleted().
		Exec(ctx)

	return err
}

// Truncate removes all records from the table.
// WARNING: This is a destructive operation and cannot be undone.
// Use with extreme caution, typically only in tests.
//
// Example:
//
//	err := repo.Truncate(ctx)
func (r *Repository[T]) Truncate(ctx context.Context) error {
	var entity T

	_, err := r.db.NewTruncateTable().Model(&entity).Exec(ctx)

	return err
}

// Common QueryOptions that can be reused across repositories.

// WithLimit returns a QueryOption that limits the number of results.
func WithLimit(limit int) QueryOption {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Limit(limit)
	}
}

// WithOffset returns a QueryOption that skips the first n results.
func WithOffset(offset int) QueryOption {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Offset(offset)
	}
}

// WithOrder returns a QueryOption that orders results by the specified column.
func WithOrder(column string, direction ...string) QueryOption {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		order := column
		if len(direction) > 0 {
			order = fmt.Sprintf("%s %s", column, direction[0])
		}

		return q.Order(order)
	}
}

// WhereActive returns a QueryOption that filters out soft-deleted records.
// This is explicit - records are NOT filtered automatically.
func WhereActive() QueryOption {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("deleted_at IS NULL")
	}
}

// WhereDeleted returns a QueryOption that only returns soft-deleted records.
func WhereDeleted() QueryOption {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("deleted_at IS NOT NULL").WhereAllWithDeleted()
	}
}

// WithRelation returns a QueryOption that eager loads a relation.
func WithRelation(relation string) QueryOption {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Relation(relation)
	}
}

// WithRelations returns a QueryOption that eager loads multiple relations.
func WithRelations(relations ...string) QueryOption {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		for _, rel := range relations {
			q = q.Relation(rel)
		}

		return q
	}
}
