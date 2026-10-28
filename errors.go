package sessionmanager

import "errors"

var (
	// ErrMiddlewareMissing means no Manager is installed on the current request.
	ErrMiddlewareMissing = errors.New("session middleware missing")
	// ErrUnknownNamespace means the requested namespace was not registered.
	ErrUnknownNamespace = errors.New("unknown session namespace")
	// ErrManagerConflict means different Managers entered the same request chain.
	ErrManagerConflict = errors.New("conflicting session manager")
	// ErrKeyNotFound means the requested data key does not exist.
	ErrKeyNotFound = errors.New("session key not found")
	// ErrInvalidRecord means a stored or encoded envelope violates the record format.
	ErrInvalidRecord = errors.New("invalid session record")
	// ErrExpired means a save would exceed the session's absolute deadline.
	ErrExpired = errors.New("session absolute timeout exceeded")
	// ErrSessionNotFound means GetByID found no live session for the supplied ID.
	ErrSessionNotFound = errors.New("session not found")
	// ErrInvalidSessionID means an ID is empty, unsuitable for its transport,
	// or was reused by a generator when a different ID was required.
	ErrInvalidSessionID = errors.New("invalid session ID")
)
