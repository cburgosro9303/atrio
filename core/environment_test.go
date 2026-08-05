package core_test

import (
	"testing"

	"github.com/cburgosro9303/atrio/core"
)

func TestValidateClosureEnvironment(t *testing.T) {
	t.Parallel()

	environments := []string{"dev", "staging", "prod"}

	cases := []struct {
		name    string
		closure string
		wantErr bool
	}{
		{"a declared environment is legal", "staging", false},
		{"the first declared environment is legal", "dev", false},
		{"the last declared environment is legal", "prod", false},
		{"an undeclared environment is illegal", "qa", true},
		{"an empty closure environment is illegal", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := core.ValidateClosureEnvironment(tc.closure, environments)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateClosureEnvironment(%q, %v) error = %v, wantErr %v", tc.closure, environments, err, tc.wantErr)
			}
		})
	}
}
