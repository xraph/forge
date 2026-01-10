package database

import (
	"fmt"
	"testing"
)

type UserDTO struct {
	ID    int64
	Name  string
	Email string
}

func TestMapTo(t *testing.T) {
	user := TestUser{ID: 1, Name: "John", Email: "john@example.com", Age: 30}

	dto := MapTo(user, func(u TestUser) UserDTO {
		return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email}
	})

	if dto.ID != user.ID {
		t.Errorf("expected ID %d, got %d", user.ID, dto.ID)
	}

	if dto.Name != user.Name {
		t.Errorf("expected name %s, got %s", user.Name, dto.Name)
	}
}

func TestMapSlice(t *testing.T) {
	users := []TestUser{
		{ID: 1, Name: "User1", Email: "user1@example.com"},
		{ID: 2, Name: "User2", Email: "user2@example.com"},
		{ID: 3, Name: "User3", Email: "user3@example.com"},
	}

	dtos := MapSlice(users, func(u TestUser) UserDTO {
		return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email}
	})

	if len(dtos) != 3 {
		t.Errorf("expected 3 DTOs, got %d", len(dtos))
	}

	for i, dto := range dtos {
		if dto.ID != users[i].ID {
			t.Errorf("expected ID %d, got %d", users[i].ID, dto.ID)
		}
	}
}

func TestMapSliceNil(t *testing.T) {
	var users []TestUser = nil

	dtos := MapSlice(users, func(u TestUser) UserDTO {
		return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email}
	})

	if dtos != nil {
		t.Error("expected nil result for nil input")
	}
}

func TestMapPaginated(t *testing.T) {
	result := &PaginatedResult[TestUser]{
		Data: []TestUser{
			{ID: 1, Name: "User1", Email: "user1@example.com"},
			{ID: 2, Name: "User2", Email: "user2@example.com"},
		},
		Page:       1,
		PageSize:   10,
		TotalPages: 1,
		TotalCount: 2,
		HasNext:    false,
		HasPrev:    false,
	}

	dtoResult := MapPaginated(result, func(u TestUser) UserDTO {
		return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email}
	})

	if len(dtoResult.Data) != 2 {
		t.Errorf("expected 2 DTOs, got %d", len(dtoResult.Data))
	}

	if dtoResult.TotalCount != result.TotalCount {
		t.Errorf("expected total count %d, got %d", result.TotalCount, dtoResult.TotalCount)
	}
}

func TestMapSliceError(t *testing.T) {
	users := []TestUser{
		{ID: 1, Name: "User1", Email: "user1@example.com"},
		{ID: 2, Name: "User2", Email: ""},
		{ID: 3, Name: "User3", Email: "user3@example.com"},
	}

	// Should fail on User2 with empty email
	_, err := MapSliceError(users, func(u TestUser) (UserDTO, error) {
		if u.Email == "" {
			return UserDTO{}, fmt.Errorf("email required for user %d", u.ID)
		}

		return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email}, nil
	})
	if err == nil {
		t.Error("expected error for user with empty email")
	}
}

func TestFilterSlice(t *testing.T) {
	users := []TestUser{
		{ID: 1, Name: "User1", Age: 20},
		{ID: 2, Name: "User2", Age: 30},
		{ID: 3, Name: "User3", Age: 40},
	}

	filtered := FilterSlice(users, func(u TestUser) bool {
		return u.Age >= 30
	})

	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered users, got %d", len(filtered))
	}
}

func TestGroupBy(t *testing.T) {
	users := []TestUser{
		{ID: 1, Name: "User1", Age: 20},
		{ID: 2, Name: "User2", Age: 20},
		{ID: 3, Name: "User3", Age: 30},
	}

	grouped := GroupBy(users, func(u TestUser) int {
		return u.Age
	})

	if len(grouped[20]) != 2 {
		t.Errorf("expected 2 users with age 20, got %d", len(grouped[20]))
	}

	if len(grouped[30]) != 1 {
		t.Errorf("expected 1 user with age 30, got %d", len(grouped[30]))
	}
}

func TestIndexBy(t *testing.T) {
	users := []TestUser{
		{ID: 1, Name: "User1"},
		{ID: 2, Name: "User2"},
		{ID: 3, Name: "User3"},
	}

	indexed := IndexBy(users, func(u TestUser) int64 {
		return u.ID
	})

	if len(indexed) != 3 {
		t.Errorf("expected 3 indexed users, got %d", len(indexed))
	}

	if indexed[2].Name != "User2" {
		t.Errorf("expected User2, got %s", indexed[2].Name)
	}
}

func TestPluck(t *testing.T) {
	users := []TestUser{
		{ID: 1, Name: "User1"},
		{ID: 2, Name: "User2"},
		{ID: 3, Name: "User3"},
	}

	ids := Pluck(users, func(u TestUser) int64 {
		return u.ID
	})

	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(ids))
	}

	for i, id := range ids {
		if id != users[i].ID {
			t.Errorf("expected ID %d, got %d", users[i].ID, id)
		}
	}
}

func TestUnique(t *testing.T) {
	users := []TestUser{
		{ID: 1, Name: "User1"},
		{ID: 2, Name: "User2"},
		{ID: 1, Name: "User1 Duplicate"},
	}

	unique := Unique(users, func(u TestUser) int64 {
		return u.ID
	})

	if len(unique) != 2 {
		t.Errorf("expected 2 unique users, got %d", len(unique))
	}
}

func TestPartition(t *testing.T) {
	users := []TestUser{
		{ID: 1, Age: 20},
		{ID: 2, Age: 30},
		{ID: 3, Age: 40},
		{ID: 4, Age: 25},
	}

	young, old := Partition(users, func(u TestUser) bool {
		return u.Age < 30
	})

	if len(young) != 2 {
		t.Errorf("expected 2 young users, got %d", len(young))
	}

	if len(old) != 2 {
		t.Errorf("expected 2 old users, got %d", len(old))
	}
}
