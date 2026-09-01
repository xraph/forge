package pathspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstraint_Match(t *testing.T) {
	tests := []struct {
		name       string
		constraint Constraint
		enum       []string
		value      string
		want       bool
	}{
		{name: "none accepts anything", constraint: ConstraintNone, value: "anything-at-all", want: true},
		{name: "none rejects empty", constraint: ConstraintNone, value: "", want: false},

		{name: "int accepts digits", constraint: ConstraintInt, value: "42", want: true},
		{name: "int accepts negative", constraint: ConstraintInt, value: "-42", want: true},
		{name: "int accepts explicit positive", constraint: ConstraintInt, value: "+42", want: true},
		{name: "int rejects a bare sign", constraint: ConstraintInt, value: "-", want: false},
		{name: "int rejects letters", constraint: ConstraintInt, value: "4a2", want: false},

		{name: "uint accepts digits", constraint: ConstraintUint, value: "42", want: true},
		{name: "uint rejects negative", constraint: ConstraintUint, value: "-42", want: false},

		{name: "alpha accepts letters", constraint: ConstraintAlpha, value: "me", want: true},
		{name: "alpha rejects digits", constraint: ConstraintAlpha, value: "me2", want: false},

		{name: "alnum accepts a mix", constraint: ConstraintAlnum, value: "abc123", want: true},
		{name: "alnum rejects a hyphen", constraint: ConstraintAlnum, value: "abc-123", want: false},

		{
			name:       "uuid accepts a canonical uuid",
			constraint: ConstraintUUID,
			value:      "123e4567-e89b-12d3-a456-426614174000",
			want:       true,
		},
		{
			name:       "uuid rejects a wrong-length group",
			constraint: ConstraintUUID,
			value:      "123e4567-e89b-12d3-a456-42661417400",
			want:       false,
		},
		{name: "uuid rejects a non-hex character", constraint: ConstraintUUID, value: "123e4567-e89b-12d3-a456-42661417400g", want: false},
		{name: "uuid rejects an unhyphenated uuid", constraint: ConstraintUUID, value: "123e4567e89b12d3a456426614174000", want: false},

		{name: "enum accepts a listed value", constraint: ConstraintEnum, enum: []string{"draft", "sent"}, value: "sent", want: true},
		{name: "enum rejects an unlisted value", constraint: ConstraintEnum, enum: []string{"draft", "sent"}, value: "paid", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.constraint.Match(tt.value, tt.enum))
		})
	}
}

// Rank drives which parameter edge the matcher tries first. The spec fixes the
// order as enum > uuid > int,uint > alpha,alnum > none.
func TestConstraint_RankOrdering(t *testing.T) {
	assert.Greater(t, ConstraintEnum.Rank(), ConstraintUUID.Rank())
	assert.Greater(t, ConstraintUUID.Rank(), ConstraintInt.Rank())
	assert.Equal(t, ConstraintInt.Rank(), ConstraintUint.Rank())
	assert.Greater(t, ConstraintInt.Rank(), ConstraintAlpha.Rank())
	assert.Equal(t, ConstraintAlpha.Rank(), ConstraintAlnum.Rank())
	assert.Greater(t, ConstraintAlpha.Rank(), ConstraintNone.Rank())
}

func TestConstraint_ByName(t *testing.T) {
	for name, want := range map[string]Constraint{
		"int":   ConstraintInt,
		"uint":  ConstraintUint,
		"uuid":  ConstraintUUID,
		"alpha": ConstraintAlpha,
		"alnum": ConstraintAlnum,
	} {
		got, ok := constraintByName(name)
		require.Truef(t, ok, "constraint %q should be known", name)
		assert.Equal(t, want, got)
		assert.Equal(t, name, got.String(), "String must round-trip the parse name")
	}

	_, ok := constraintByName("regex")
	assert.False(t, ok, "the vocabulary is closed; regex must not be recognized")
}
