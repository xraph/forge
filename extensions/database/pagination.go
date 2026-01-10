package database

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/uptrace/bun"
	"github.com/xraph/forge/errors"
)

// OffsetPagination configures offset-based pagination.
// This is the traditional page number approach, suitable for most use cases
// with moderate data sizes (up to hundreds of thousands of records).
//
// Example:
//
//	params := database.OffsetPagination{
//	    Page:     1,
//	    PageSize: 20,
//	    Sort:     "created_at",
//	    Order:    "desc",
//	}
type OffsetPagination struct {
	// Page number (1-indexed). Defaults to 1 if not specified.
	Page int `json:"page"`

	// Number of records per page. Defaults to 20 if not specified.
	PageSize int `json:"page_size"`

	// Column to sort by. If empty, no ordering is applied.
	Sort string `json:"sort,omitempty"`

	// Sort order: "asc" or "desc". Defaults to "asc".
	Order string `json:"order,omitempty"`
}

// Normalize ensures pagination parameters are within valid ranges.
func (p *OffsetPagination) Normalize() {
	if p.Page < 1 {
		p.Page = 1
	}

	if p.PageSize < 1 {
		p.PageSize = 20
	}

	if p.PageSize > 1000 {
		p.PageSize = 1000 // Cap at 1000 to prevent abuse
	}

	if p.Order != "asc" && p.Order != "desc" {
		p.Order = "asc"
	}
}

// Offset calculates the SQL OFFSET value from page and page size.
func (p *OffsetPagination) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// PaginatedResult contains the paginated data and metadata.
type PaginatedResult[T any] struct {
	// Data contains the records for the current page
	Data []T `json:"data"`

	// Page is the current page number (1-indexed)
	Page int `json:"page"`

	// PageSize is the number of records per page
	PageSize int `json:"page_size"`

	// TotalPages is the total number of pages
	TotalPages int `json:"total_pages"`

	// TotalCount is the total number of records across all pages
	TotalCount int64 `json:"total_count"`

	// HasNext indicates if there's a next page
	HasNext bool `json:"has_next"`

	// HasPrev indicates if there's a previous page
	HasPrev bool `json:"has_prev"`
}

// Paginate performs offset-based pagination on a Bun SelectQuery.
// It executes two queries: one for the total count and one for the data.
//
// Example:
//
//	query := db.NewSelect().Model((*User)(nil))
//	result, err := database.Paginate[User](ctx, query, database.OffsetPagination{
//	    Page:     2,
//	    PageSize: 20,
//	    Sort:     "created_at",
//	    Order:    "desc",
//	})
//	if err != nil {
//	    return err
//	}
//	// result.Data contains the users for page 2
//	// result.TotalCount contains the total number of users
func Paginate[T any](ctx context.Context, query *bun.SelectQuery, params OffsetPagination) (*PaginatedResult[T], error) {
	params.Normalize()

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}

	// Calculate total pages
	totalPages := max(int(math.Ceil(float64(totalCount)/float64(params.PageSize))), 1)

	// Fetch paginated data
	var data []T

	dataQuery := query.Limit(params.PageSize).Offset(params.Offset())

	// Apply sorting if specified
	if params.Sort != "" {
		order := fmt.Sprintf("%s %s", params.Sort, params.Order)
		dataQuery = dataQuery.Order(order)
	}

	err = dataQuery.Scan(ctx, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch paginated data: %w", err)
	}

	// Handle nil slice
	if data == nil {
		data = []T{}
	}

	return &PaginatedResult[T]{
		Data:       data,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
		TotalCount: int64(totalCount),
		HasNext:    params.Page < totalPages,
		HasPrev:    params.Page > 1,
	}, nil
}

// CursorPagination configures cursor-based pagination.
// This approach is more efficient for large datasets and provides stable pagination
// even when data changes between requests.
//
// Cursor format: base64(sort_value:id)
//
// Example:
//
//	params := database.CursorPagination{
//	    Cursor:    "eyJjcmVhdGVkX2F0IjoiMjAyNC0wMS0wMVQxMDowMDowMFoiLCJpZCI6MTIzfQ==",
//	    PageSize:  20,
//	    Sort:      "created_at",
//	    Direction: "forward",
//	}
type CursorPagination struct {
	// Cursor is the base64-encoded position to start from.
	// Empty string starts from the beginning (forward) or end (backward).
	Cursor string `json:"cursor,omitempty"`

	// PageSize is the number of records to return. Defaults to 20.
	PageSize int `json:"page_size"`

	// Sort is the column to sort by. Required for cursor pagination.
	Sort string `json:"sort"`

	// Direction is either "forward" (next page) or "backward" (previous page).
	// Defaults to "forward".
	Direction string `json:"direction,omitempty"`
}

// Normalize ensures cursor pagination parameters are valid.
func (c *CursorPagination) Normalize() {
	if c.PageSize < 1 {
		c.PageSize = 20
	}

	if c.PageSize > 1000 {
		c.PageSize = 1000
	}

	if c.Direction != "forward" && c.Direction != "backward" {
		c.Direction = "forward"
	}
}

// cursorData represents the decoded cursor information.
type cursorData struct {
	Value any   `json:"value"` // The sort column value
	ID    int64 `json:"id"`    // The record ID for uniqueness
}

// CursorResult contains cursor-paginated data and navigation cursors.
type CursorResult[T any] struct {
	// Data contains the records for the current page
	Data []T `json:"data"`

	// NextCursor is the cursor for the next page (nil if no next page)
	NextCursor *string `json:"next_cursor,omitempty"`

	// PrevCursor is the cursor for the previous page (nil if no previous page)
	PrevCursor *string `json:"prev_cursor,omitempty"`

	// HasNext indicates if there's a next page
	HasNext bool `json:"has_next"`

	// HasPrev indicates if there's a previous page
	HasPrev bool `json:"has_prev"`
}

// PaginateCursor performs cursor-based pagination on a Bun SelectQuery.
// This is more efficient for large datasets and provides stable pagination.
//
// Example:
//
//	query := db.NewSelect().Model((*User)(nil))
//	result, err := database.PaginateCursor[User](ctx, query, database.CursorPagination{
//	    Cursor:    cursor,
//	    PageSize:  20,
//	    Sort:      "created_at",
//	    Direction: "forward",
//	})
func PaginateCursor[T any](ctx context.Context, query *bun.SelectQuery, params CursorPagination) (*CursorResult[T], error) {
	params.Normalize()

	if params.Sort == "" {
		return nil, errors.New("sort column is required for cursor pagination")
	}

	// Decode cursor if provided
	var cursor *cursorData

	if params.Cursor != "" {
		decoded, err := decodeCursor(params.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}

		cursor = decoded
	}

	// Fetch one extra record to determine if there's a next/prev page
	limit := params.PageSize + 1

	// Apply cursor condition
	if cursor != nil {
		if params.Direction == "forward" {
			// For forward pagination: WHERE (sort_col, id) > (cursor.Value, cursor.ID)
			query = query.Where("(?, ?) > (?, ?)", bun.Ident(params.Sort), bun.Ident("id"), cursor.Value, cursor.ID)
		} else {
			// For backward pagination: WHERE (sort_col, id) < (cursor.Value, cursor.ID)
			query = query.Where("(?, ?) < (?, ?)", bun.Ident(params.Sort), bun.Ident("id"), cursor.Value, cursor.ID)
		}
	}

	// Apply ordering
	sortOrder := "ASC"
	if params.Direction == "backward" {
		sortOrder = "DESC"
	}

	query = query.Order(fmt.Sprintf("%s %s, id %s", params.Sort, sortOrder, sortOrder))

	// Fetch data
	query = query.Limit(limit)

	var data []T

	err := query.Scan(ctx, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cursor paginated data: %w", err)
	}

	// Handle nil slice
	if data == nil {
		data = []T{}
	}

	// Determine if there are more pages
	hasMore := len(data) > params.PageSize
	if hasMore {
		// Remove the extra record
		data = data[:params.PageSize]
	}

	// For backward pagination, reverse the results
	if params.Direction == "backward" {
		for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
			data[i], data[j] = data[j], data[i]
		}
	}

	result := &CursorResult[T]{
		Data: data,
	}

	// Generate cursors if we have data
	if len(data) > 0 {
		// For forward pagination
		if params.Direction == "forward" {
			result.HasPrev = cursor != nil
			result.HasNext = hasMore

			if result.HasNext {
				// Next cursor is the last item
				lastCursor, err := createCursor(data[len(data)-1], params.Sort)
				if err == nil {
					result.NextCursor = &lastCursor
				}
			}

			if result.HasPrev {
				// Prev cursor is the first item
				firstCursor, err := createCursor(data[0], params.Sort)
				if err == nil {
					result.PrevCursor = &firstCursor
				}
			}
		} else {
			// For backward pagination
			result.HasNext = cursor != nil
			result.HasPrev = hasMore

			if result.HasPrev {
				// Prev cursor is the first item (after reversal)
				firstCursor, err := createCursor(data[0], params.Sort)
				if err == nil {
					result.PrevCursor = &firstCursor
				}
			}

			if result.HasNext {
				// Next cursor is the last item (after reversal)
				lastCursor, err := createCursor(data[len(data)-1], params.Sort)
				if err == nil {
					result.NextCursor = &lastCursor
				}
			}
		}
	}

	return result, nil
}

// createCursor generates a base64-encoded cursor from a record.
func createCursor(record any, sortField string) (string, error) {
	// Use reflection to extract the sort field value and ID
	// This is a simplified implementation - in production you might want more sophisticated field extraction
	recordMap, err := structToMap(record)
	if err != nil {
		return "", err
	}

	sortValue, ok := recordMap[sortField]
	if !ok {
		// Try uppercase version
		sortValue, ok = recordMap[strings.Title(sortField)]
		if !ok {
			return "", fmt.Errorf("sort field %s not found in record", sortField)
		}
	}

	// Try lowercase first, then uppercase (handles both json:"id" and no json tag)
	id, ok := recordMap["id"]
	if !ok {
		id, ok = recordMap["ID"]
		if !ok {
			return "", errors.New("id field not found in record")
		}
	}

	cursor := cursorData{
		Value: sortValue,
		ID:    toInt64(id),
	}

	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(data), nil
}

// decodeCursor decodes a base64-encoded cursor.
func decodeCursor(encoded string) (*cursorData, error) {
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	var cursor cursorData

	err = json.Unmarshal(data, &cursor)
	if err != nil {
		return nil, err
	}

	return &cursor, nil
}

// structToMap converts a struct to a map using JSON marshaling.
// This is a simple approach that works for most cases.
func structToMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var m map[string]any

	err = json.Unmarshal(data, &m)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// toInt64 converts various numeric types to int64.
func toInt64(v any) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	default:
		return 0
	}
}
