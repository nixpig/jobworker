package jobmanager

import (
	"errors"
	"fmt"
)

var (
	ErrJobNotFound = errors.New("job not found")
)

// InvalidStateError is returned when attempting an invalid Job state
// transition.
type InvalidStateError struct {
	from JobState
	to   JobState
}

func (e InvalidStateError) Error() string {
	return fmt.Sprintf("cannot go from %s to %s", e.from, e.to)
}

func NewInvalidStateError(from, to JobState) InvalidStateError {
	return InvalidStateError{from, to}
}
