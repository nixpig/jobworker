package main

import (
	"strings"
	"testing"

	api "github.com/nixpig/jobworker/api/v1"
)

func TestCliHelpers(t *testing.T) {
	t.Parallel()

	t.Run("Test all job states are mapped", func(t *testing.T) {
		for v := range api.JobState_name {
			if strings.Contains(mapState(api.JobState(v)), "Unknown") {
				t.Errorf("unmapped job state: '%v'", v)
			}
		}
	})

	t.Run("Test unknown job state", func(t *testing.T) {
		gotMappedState := mapState(api.JobState(999))

		if !strings.Contains(gotMappedState, "Unknown(999)") {
			t.Errorf("expected unknown job state: got '%v'", gotMappedState)
		}
	})
}
