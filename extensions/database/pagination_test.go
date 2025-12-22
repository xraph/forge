package database

import (
	"context"
	"testing"
)

func TestPaginate(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Create 25 test users
	for i := 1; i <= 25; i++ {
		user := &TestUser{
			Name:  "User" + string(rune(i)),
			Email: "user" + string(rune(i)) + "@example.com",
			Age:   20 + i,
		}
		_, err := db.NewInsert().Model(user).Exec(ctx)
		AssertNoDatabaseError(t, err)
	}

	// Test first page
	query := db.NewSelect().Model((*TestUser)(nil))
	result, err := Paginate[TestUser](ctx, query, OffsetPagination{
		Page:     1,
		PageSize: 10,
		Sort:     "id",
		Order:    "asc",
	})
	AssertNoDatabaseError(t, err)

	if len(result.Data) != 10 {
		t.Errorf("expected 10 items on first page, got %d", len(result.Data))
	}

	if result.TotalCount != 25 {
		t.Errorf("expected total count 25, got %d", result.TotalCount)
	}

	if result.TotalPages != 3 {
		t.Errorf("expected 3 total pages, got %d", result.TotalPages)
	}

	if !result.HasNext {
		t.Error("expected HasNext to be true")
	}

	if result.HasPrev {
		t.Error("expected HasPrev to be false on first page")
	}

	// Test second page
	query = db.NewSelect().Model((*TestUser)(nil))
	result, err = Paginate[TestUser](ctx, query, OffsetPagination{
		Page:     2,
		PageSize: 10,
		Sort:     "id",
		Order:    "asc",
	})
	AssertNoDatabaseError(t, err)

	if len(result.Data) != 10 {
		t.Errorf("expected 10 items on second page, got %d", len(result.Data))
	}

	if !result.HasNext {
		t.Error("expected HasNext to be true on second page")
	}

	if !result.HasPrev {
		t.Error("expected HasPrev to be true on second page")
	}

	// Test last page
	query = db.NewSelect().Model((*TestUser)(nil))
	result, err = Paginate[TestUser](ctx, query, OffsetPagination{
		Page:     3,
		PageSize: 10,
		Sort:     "id",
		Order:    "asc",
	})
	AssertNoDatabaseError(t, err)

	if len(result.Data) != 5 {
		t.Errorf("expected 5 items on last page, got %d", len(result.Data))
	}

	if result.HasNext {
		t.Error("expected HasNext to be false on last page")
	}

	if !result.HasPrev {
		t.Error("expected HasPrev to be true on last page")
	}
}

func TestPaginationNormalize(t *testing.T) {
	tests := []struct {
		name     string
		input    OffsetPagination
		expected OffsetPagination
	}{
		{
			name:     "negative page",
			input:    OffsetPagination{Page: -1, PageSize: 10},
			expected: OffsetPagination{Page: 1, PageSize: 10, Order: "asc"},
		},
		{
			name:     "zero page size",
			input:    OffsetPagination{Page: 1, PageSize: 0},
			expected: OffsetPagination{Page: 1, PageSize: 20, Order: "asc"},
		},
		{
			name:     "excessive page size",
			input:    OffsetPagination{Page: 1, PageSize: 2000},
			expected: OffsetPagination{Page: 1, PageSize: 1000, Order: "asc"},
		},
		{
			name:     "invalid order",
			input:    OffsetPagination{Page: 1, PageSize: 10, Order: "invalid"},
			expected: OffsetPagination{Page: 1, PageSize: 10, Order: "asc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.Normalize()

			if tt.input.Page != tt.expected.Page {
				t.Errorf("expected page %d, got %d", tt.expected.Page, tt.input.Page)
			}

			if tt.input.PageSize != tt.expected.PageSize {
				t.Errorf("expected page size %d, got %d", tt.expected.PageSize, tt.input.PageSize)
			}

			if tt.input.Order != tt.expected.Order {
				t.Errorf("expected order %s, got %s", tt.expected.Order, tt.input.Order)
			}
		})
	}
}

func TestPaginateCursor(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// Create 10 test users
	for i := 1; i <= 10; i++ {
		user := &TestUser{
			Name:  "User" + string(rune(i)),
			Email: "user" + string(rune(i)) + "@example.com",
			Age:   20 + i,
		}
		_, err := db.NewInsert().Model(user).Exec(ctx)
		AssertNoDatabaseError(t, err)
	}

	// First page (no cursor)
	query := db.NewSelect().Model((*TestUser)(nil))
	result, err := PaginateCursor[TestUser](ctx, query, CursorPagination{
		PageSize:  5,
		Sort:      "id",
		Direction: "forward",
	})
	AssertNoDatabaseError(t, err)

	if len(result.Data) != 5 {
		t.Errorf("expected 5 items, got %d", len(result.Data))
	}

	if !result.HasNext {
		t.Error("expected HasNext to be true")
	}

	if result.HasPrev {
		t.Error("expected HasPrev to be false on first page")
	}

	if result.NextCursor == nil {
		t.Fatal("expected NextCursor to be set")
	}

	// Second page (using cursor)
	query = db.NewSelect().Model((*TestUser)(nil))
	result, err = PaginateCursor[TestUser](ctx, query, CursorPagination{
		Cursor:    *result.NextCursor,
		PageSize:  5,
		Sort:      "id",
		Direction: "forward",
	})
	AssertNoDatabaseError(t, err)

	if len(result.Data) != 5 {
		t.Errorf("expected 5 items on second page, got %d", len(result.Data))
	}

	if result.HasNext {
		t.Error("expected HasNext to be false on last page")
	}

	if !result.HasPrev {
		t.Error("expected HasPrev to be true on second page")
	}
}

func TestPaginationEmptyResult(t *testing.T) {
	db := NewTestDB(t, WithAutoMigrate(&TestUser{}))
	ctx := context.Background()

	// No data in database
	query := db.NewSelect().Model((*TestUser)(nil))
	result, err := Paginate[TestUser](ctx, query, OffsetPagination{
		Page:     1,
		PageSize: 10,
	})
	AssertNoDatabaseError(t, err)

	if len(result.Data) != 0 {
		t.Errorf("expected 0 items, got %d", len(result.Data))
	}

	if result.TotalCount != 0 {
		t.Errorf("expected total count 0, got %d", result.TotalCount)
	}

	if result.HasNext {
		t.Error("expected HasNext to be false")
	}

	if result.HasPrev {
		t.Error("expected HasPrev to be false")
	}
}
