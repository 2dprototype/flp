package pyflp

import (
	"errors"
	"fmt"
)

// Sentinel errors; use errors.Is to test for a family of errors.
var (
	// ErrFLP is the root of every error produced by this package.
	ErrFLP = errors.New("flp")
	// ErrDataCorrupted is the base of parsing errors.
	ErrDataCorrupted = fmt.Errorf("%w: data corrupted", ErrFLP)
	// ErrHeaderCorrupted: the header chunk contains an invalid value.
	ErrHeaderCorrupted = fmt.Errorf("%w: header corrupted", ErrDataCorrupted)
	// ErrNoModelsFound: a collection could not find any of its models.
	ErrNoModelsFound = fmt.Errorf("%w: no models found", ErrDataCorrupted)
	// ErrModelNotFound: an invalid index or name was passed to a collection.
	ErrModelNotFound = fmt.Errorf("%w: model not found", ErrDataCorrupted)
	// ErrChannelNotFound is a specialisation of ErrModelNotFound.
	ErrChannelNotFound = fmt.Errorf("%w: channel not found", ErrModelNotFound)
	// ErrVersionNotDetected: string decoder could not be decided as the
	// project version is missing.
	ErrVersionNotDetected = fmt.Errorf("%w: version not detected", ErrDataCorrupted)
	// ErrEventIDOutOfRange: an event was created with an ID out of its range.
	ErrEventIDOutOfRange = fmt.Errorf("%w: event id out of range", ErrFLP)
	// ErrInvalidEventChunkSize: a fixed size event got the wrong byte count.
	ErrInvalidEventChunkSize = fmt.Errorf("%w: invalid event chunk size", ErrFLP)
	// ErrPropertyCannotBeSet: the underlying event(s) were not found.
	ErrPropertyCannotBeSet = fmt.Errorf("%w: property cannot be set", ErrFLP)
	// ErrEventNotFound corresponds to PyFLP's KeyError on EventTree.First.
	ErrEventNotFound = fmt.Errorf("%w: event not found", ErrFLP)
	// ErrInvalidValue is returned for values outside the permitted range.
	ErrInvalidValue = fmt.Errorf("%w: invalid value", ErrFLP)
)

func headerCorrupted(desc string) error {
	return fmt.Errorf("%w: error parsing header: %s", ErrHeaderCorrupted, desc)
}

func eventIDOutOfRange(id EventID, expected string) error {
	return fmt.Errorf("%w: expected ID in %s; got %d instead", ErrEventIDOutOfRange, expected, int(id))
}

func invalidChunkSize(expected, got int) error {
	return fmt.Errorf("%w: expected a bytes object of length %d; got %d", ErrInvalidEventChunkSize, expected, got)
}

func modelNotFound(key interface{}) error {
	return fmt.Errorf("%w: %v", ErrModelNotFound, key)
}

func channelNotFound(key interface{}) error {
	return fmt.Errorf("%w: %v", ErrChannelNotFound, key)
}

func cannotSet(ids ...EventID) error {
	if len(ids) == 0 {
		return ErrPropertyCannotBeSet
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = IDName(id)
	}
	return fmt.Errorf("%w: event(s) %v was / were not found", ErrPropertyCannotBeSet, names)
}

func invalidValue(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidValue, fmt.Sprintf(format, args...))
}

// WarningHandler receives non-fatal warnings (the equivalent of Python's
// warnings.warn in PyFLP). It is nil by default, which silences warnings.
var WarningHandler func(msg string)

func warn(format string, args ...interface{}) {
	if WarningHandler != nil {
		WarningHandler(fmt.Sprintf(format, args...))
	}
}
