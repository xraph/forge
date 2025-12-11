package database

// MapTo transforms a single value from one type to another using a mapper function.
// This is useful for converting database models to DTOs.
//
// Example:
//
//	userDTO := database.MapTo(user, func(u User) UserDTO {
//	    return UserDTO{
//	        ID:    u.ID,
//	        Name:  u.Name,
//	        Email: u.Email,
//	    }
//	})
func MapTo[From, To any](from From, mapFn func(From) To) To {
	return mapFn(from)
}

// MapSlice transforms a slice of values from one type to another.
// This eliminates the need for manual loops when converting query results.
//
// Example:
//
//	users, err := repo.FindAll(ctx)
//	dtos := database.MapSlice(users, func(u User) UserDTO {
//	    return UserDTO{ID: u.ID, Name: u.Name}
//	})
func MapSlice[From, To any](from []From, mapFn func(From) To) []To {
	if from == nil {
		return nil
	}

	result := make([]To, len(from))
	for i, item := range from {
		result[i] = mapFn(item)
	}

	return result
}

// MapPaginated transforms a PaginatedResult from one type to another.
// The pagination metadata is preserved while the data is transformed.
//
// Example:
//
//	result, err := database.Paginate[User](ctx, query, params)
//	dtoResult := database.MapPaginated(result, func(u User) UserDTO {
//	    return UserDTO{ID: u.ID, Name: u.Name}
//	})
func MapPaginated[From, To any](from *PaginatedResult[From], mapFn func(From) To) *PaginatedResult[To] {
	if from == nil {
		return nil
	}

	return &PaginatedResult[To]{
		Data:       MapSlice(from.Data, mapFn),
		Page:       from.Page,
		PageSize:   from.PageSize,
		TotalPages: from.TotalPages,
		TotalCount: from.TotalCount,
		HasNext:    from.HasNext,
		HasPrev:    from.HasPrev,
	}
}

// MapCursor transforms a CursorResult from one type to another.
// The cursor metadata is preserved while the data is transformed.
//
// Example:
//
//	result, err := database.PaginateCursor[User](ctx, query, params)
//	dtoResult := database.MapCursor(result, func(u User) UserDTO {
//	    return UserDTO{ID: u.ID, Name: u.Name}
//	})
func MapCursor[From, To any](from *CursorResult[From], mapFn func(From) To) *CursorResult[To] {
	if from == nil {
		return nil
	}

	return &CursorResult[To]{
		Data:       MapSlice(from.Data, mapFn),
		NextCursor: from.NextCursor,
		PrevCursor: from.PrevCursor,
		HasNext:    from.HasNext,
		HasPrev:    from.HasPrev,
	}
}

// MapPointer transforms a pointer value, handling nil safely.
//
// Example:
//
//	userDTO := database.MapPointer(userPtr, func(u User) UserDTO {
//	    return UserDTO{ID: u.ID, Name: u.Name}
//	})
func MapPointer[From, To any](from *From, mapFn func(From) To) *To {
	if from == nil {
		return nil
	}

	result := mapFn(*from)
	return &result
}

// MapSlicePointers transforms a slice of pointers.
//
// Example:
//
//	dtos := database.MapSlicePointers(userPtrs, func(u User) UserDTO {
//	    return UserDTO{ID: u.ID, Name: u.Name}
//	})
func MapSlicePointers[From, To any](from []*From, mapFn func(From) To) []To {
	if from == nil {
		return nil
	}

	result := make([]To, 0, len(from))
	for _, item := range from {
		if item != nil {
			result = append(result, mapFn(*item))
		}
	}

	return result
}

// MapError applies a mapper function and returns both the result and any error.
// Useful when the mapping function can fail.
//
// Example:
//
//	dto, err := database.MapError(user, func(u User) (UserDTO, error) {
//	    if u.Email == "" {
//	        return UserDTO{}, fmt.Errorf("email is required")
//	    }
//	    return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email}, nil
//	})
func MapError[From, To any](from From, mapFn func(From) (To, error)) (To, error) {
	return mapFn(from)
}

// MapSliceError transforms a slice with a fallible mapper function.
// If any mapping fails, the function returns immediately with the error.
//
// Example:
//
//	dtos, err := database.MapSliceError(users, func(u User) (UserDTO, error) {
//	    if u.Email == "" {
//	        return UserDTO{}, fmt.Errorf("user %d: email is required", u.ID)
//	    }
//	    return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email}, nil
//	})
func MapSliceError[From, To any](from []From, mapFn func(From) (To, error)) ([]To, error) {
	if from == nil {
		return nil, nil
	}

	result := make([]To, len(from))
	for i, item := range from {
		mapped, err := mapFn(item)
		if err != nil {
			return nil, err
		}
		result[i] = mapped
	}

	return result, nil
}

// MapPaginatedError transforms a PaginatedResult with a fallible mapper.
//
// Example:
//
//	dtoResult, err := database.MapPaginatedError(result, func(u User) (UserDTO, error) {
//	    return toDTO(u)
//	})
func MapPaginatedError[From, To any](from *PaginatedResult[From], mapFn func(From) (To, error)) (*PaginatedResult[To], error) {
	if from == nil {
		return nil, nil
	}

	data, err := MapSliceError(from.Data, mapFn)
	if err != nil {
		return nil, err
	}

	return &PaginatedResult[To]{
		Data:       data,
		Page:       from.Page,
		PageSize:   from.PageSize,
		TotalPages: from.TotalPages,
		TotalCount: from.TotalCount,
		HasNext:    from.HasNext,
		HasPrev:    from.HasPrev,
	}, nil
}

// MapCursorError transforms a CursorResult with a fallible mapper.
//
// Example:
//
//	dtoResult, err := database.MapCursorError(result, func(u User) (UserDTO, error) {
//	    return toDTO(u)
//	})
func MapCursorError[From, To any](from *CursorResult[From], mapFn func(From) (To, error)) (*CursorResult[To], error) {
	if from == nil {
		return nil, nil
	}

	data, err := MapSliceError(from.Data, mapFn)
	if err != nil {
		return nil, err
	}

	return &CursorResult[To]{
		Data:       data,
		NextCursor: from.NextCursor,
		PrevCursor: from.PrevCursor,
		HasNext:    from.HasNext,
		HasPrev:    from.HasPrev,
	}, nil
}

// FilterSlice filters a slice based on a predicate function.
// Useful for post-query filtering.
//
// Example:
//
//	activeUsers := database.FilterSlice(users, func(u User) bool {
//	    return u.Active
//	})
func FilterSlice[T any](slice []T, predicate func(T) bool) []T {
	if slice == nil {
		return nil
	}

	result := make([]T, 0, len(slice))
	for _, item := range slice {
		if predicate(item) {
			result = append(result, item)
		}
	}

	return result
}

// GroupBy groups a slice by a key function.
// Returns a map where each key maps to a slice of items with that key.
//
// Example:
//
//	byStatus := database.GroupBy(users, func(u User) string {
//	    return u.Status
//	})
//	activeUsers := byStatus["active"]
func GroupBy[T any, K comparable](slice []T, keyFn func(T) K) map[K][]T {
	if slice == nil {
		return nil
	}

	result := make(map[K][]T)
	for _, item := range slice {
		key := keyFn(item)
		result[key] = append(result[key], item)
	}

	return result
}

// IndexBy creates a map from a slice using a key function.
// If multiple items have the same key, the last one wins.
//
// Example:
//
//	usersByID := database.IndexBy(users, func(u User) int64 {
//	    return u.ID
//	})
//	user := usersByID[123]
func IndexBy[T any, K comparable](slice []T, keyFn func(T) K) map[K]T {
	if slice == nil {
		return nil
	}

	result := make(map[K]T, len(slice))
	for _, item := range slice {
		key := keyFn(item)
		result[key] = item
	}

	return result
}

// Pluck extracts a specific field from each item in a slice.
//
// Example:
//
//	ids := database.Pluck(users, func(u User) int64 {
//	    return u.ID
//	})
func Pluck[T any, R any](slice []T, fn func(T) R) []R {
	if slice == nil {
		return nil
	}

	result := make([]R, len(slice))
	for i, item := range slice {
		result[i] = fn(item)
	}

	return result
}

// Unique removes duplicate values from a slice based on a key function.
//
// Example:
//
//	uniqueUsers := database.Unique(users, func(u User) int64 {
//	    return u.ID
//	})
func Unique[T any, K comparable](slice []T, keyFn func(T) K) []T {
	if slice == nil {
		return nil
	}

	seen := make(map[K]bool)
	result := make([]T, 0, len(slice))

	for _, item := range slice {
		key := keyFn(item)
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}

	return result
}

// Partition splits a slice into two slices based on a predicate.
// The first slice contains items where predicate returns true,
// the second contains items where it returns false.
//
// Example:
//
//	active, inactive := database.Partition(users, func(u User) bool {
//	    return u.Active
//	})
func Partition[T any](slice []T, predicate func(T) bool) ([]T, []T) {
	if slice == nil {
		return nil, nil
	}

	truthy := make([]T, 0)
	falsy := make([]T, 0)

	for _, item := range slice {
		if predicate(item) {
			truthy = append(truthy, item)
		} else {
			falsy = append(falsy, item)
		}
	}

	return truthy, falsy
}

