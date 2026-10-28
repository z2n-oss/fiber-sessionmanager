package sessionmanager

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"
	"time"
)

// Session holds typed values and lifecycle state for one namespace. It is not
// safe for concurrent use. HTTP sessions must not outlive their request; detached
// sessions returned by Manager.GetByID are caller-managed and need no Release.
// Zero-value and successfully destroyed Sessions are invalid and must not be used.
type Session struct {
	config    *namespaceConfig
	request   *requestBinding
	id        string
	fresh     bool
	idle      time.Duration
	expiresAt int64
	encoded   map[string][]byte
	values    map[string]any
}

func newSession(config *namespaceConfig, previous string) (*Session, error) {
	id, err := config.newID(previous)
	if err != nil {
		return nil, config.wrap("create", "", err)
	}
	deadline, err := absoluteDeadline(config.now(), config.AbsoluteTimeout)
	if err != nil {
		return nil, config.wrap("create", "", err)
	}
	return &Session{
		config: config, id: id, fresh: true, idle: config.IdleTimeout, expiresAt: deadline,
		encoded: make(map[string][]byte), values: make(map[string]any),
	}, nil
}

func (s *Session) storageKey(id string) string { return s.config.storageKey(id) }

func (s *Session) wrap(op, key string, err error) error { return s.config.wrap(op, key, err) }

// ID returns the opaque client identifier.
func (s *Session) ID() string { return s.id }

// Fresh reports whether this object has created or rotated its ID. Save does not reset it.
func (s *Session) Fresh() bool { return s.fresh }

// Set retains value and encodes it on each Get and Save, even after an earlier
// Save. Reference values must not be mutated concurrently with session operations.
func (s *Session) Set(key string, value any) { s.values[strings.Clone(key)] = value }

// Get decodes key directly into target using the configured Codec. The Codec
// validates target and may partially modify it even on error. Missing keys and
// Marshal failures leave target untouched. Modify data through Set to persist it.
func (s *Session) Get(key string, target any) error {
	raw, ok := s.encoded[key]
	if value, pending := s.values[key]; pending {
		var err error
		raw, err = s.config.Marshal(value)
		if err != nil {
			return s.wrap("encode", key, err)
		}
		ok = true
	}
	if !ok {
		return s.wrap("get", key, ErrKeyNotFound)
	}
	if err := s.config.Unmarshal(bytes.Clone(raw), target); err != nil {
		return s.wrap("decode", key, err)
	}
	return nil
}

// Delete removes a data key from pending and previously encoded values.
func (s *Session) Delete(key string) {
	delete(s.values, key)
	delete(s.encoded, key)
}

// Keys returns the merged data keys in sorted order.
func (s *Session) Keys() []string {
	keys := make(map[string]struct{}, len(s.encoded)+len(s.values))
	for key := range s.encoded {
		keys[key] = struct{}{}
	}
	for key := range s.values {
		keys[key] = struct{}{}
	}
	return slices.Sorted(maps.Keys(keys))
}

// SetIdleTimeout changes this object's subsequent save TTL. Nonpositive durations
// panic. The absolute deadline still limits each save. The override is not persisted.
func (s *Session) SetIdleTimeout(timeout time.Duration) {
	if timeout <= 0 {
		panic("sessionmanager: idle timeout must be positive")
	}
	s.idle = timeout
}

// Save persists the session and then updates any bound HTTP identifiers. It
// refreshes idle expiry and does not suppress request-end automatic saving.
// Detached sessions use context.Background(); HTTP sessions use c.Context().
func (s *Session) Save() error { return s.SaveWithContext(s.defaultContext()) }

// SaveWithContext persists using ctx (Background when nil). Encoding finishes
// before storage is touched. Backend failures may have indeterminate write effects,
// but no new client identifier is emitted until a write returns successfully.
func (s *Session) SaveWithContext(ctx context.Context) error {
	if err := validateIdentifier(s.id, s.sinks()); err != nil {
		return s.wrap("save", "", err)
	}
	values := cloneEncoded(s.encoded)
	for key, value := range s.values {
		raw, err := s.config.Marshal(value)
		if err != nil {
			return s.wrap("encode", key, err)
		}
		values[key] = bytes.Clone(raw)
	}
	raw, err := s.config.Marshal(record{Version: 1, ExpiresAt: s.expiresAt, Values: values})
	if err != nil {
		return s.wrap("encode record", "", err)
	}
	if len(raw) == 0 {
		return s.wrap("encode record", "", ErrInvalidRecord)
	}
	// A provider may retain its input after returning. Give it a buffer that
	// cannot be overwritten by the next call to a buffer-reusing Codec.
	raw = bytes.Clone(raw)
	ttl := s.idle
	if s.expiresAt != 0 {
		remaining := time.Unix(0, s.expiresAt).Sub(s.config.now())
		if remaining <= 0 {
			return s.wrap("save", "", ErrExpired)
		}
		ttl = min(ttl, remaining)
	}
	if err := s.config.Storage.SetWithContext(nonNilContext(ctx), s.storageKey(s.id), raw, ttl); err != nil {
		return s.wrap("save", "", err)
	}
	s.writeIdentifier(ttl, false)
	return nil
}

// Destroy deletes the session and invalidates this object after success. In an
// HTTP request, FromContext can then create a fresh replacement.
func (s *Session) Destroy() error { return s.DestroyWithContext(s.defaultContext()) }

// DestroyWithContext deletes using ctx. Failure leaves this object's state intact.
func (s *Session) DestroyWithContext(ctx context.Context) error {
	if err := s.config.Storage.DeleteWithContext(nonNilContext(ctx), s.storageKey(s.id)); err != nil {
		return s.wrap("destroy", "", err)
	}
	s.writeIdentifier(0, true)
	if s.request != nil {
		delete(s.request.owner.sessions, s.config.Namespace)
		s.request.owner.replaced[s.config.Namespace] = s.id
	}
	// Destroyed objects are not reusable, and need not retain values or HTTP state.
	s.config, s.request, s.encoded, s.values = nil, nil, nil, nil
	return nil
}

// Regenerate rotates the ID, retaining data and the absolute deadline. The new
// ID is persisted on Save. Detached callers must arrange client ID delivery.
func (s *Session) Regenerate() error { return s.RegenerateWithContext(s.defaultContext()) }

// RegenerateWithContext generates a different ID before deleting the old key using ctx.
func (s *Session) RegenerateWithContext(ctx context.Context) error { return s.rotate(ctx, false) }

// Reset rotates the ID, clears data and restores configured timeouts. The new
// ID and empty state are persisted on Save.
func (s *Session) Reset() error { return s.ResetWithContext(s.defaultContext()) }

// ResetWithContext validates the replacement before deleting the old key using ctx.
func (s *Session) ResetWithContext(ctx context.Context) error { return s.rotate(ctx, true) }

func (s *Session) rotate(ctx context.Context, reset bool) error {
	op := "regenerate"
	if reset {
		op = "reset"
	}
	id, err := s.config.newID(s.id)
	if err != nil {
		return s.wrap(op, "", err)
	}
	deadline := s.expiresAt
	if reset {
		deadline, err = absoluteDeadline(s.config.now(), s.config.AbsoluteTimeout)
		if err != nil {
			return s.wrap(op, "", err)
		}
	}
	if err := s.config.Storage.DeleteWithContext(nonNilContext(ctx), s.storageKey(s.id)); err != nil {
		return s.wrap(op, "", err)
	}
	s.id, s.fresh, s.expiresAt = id, true, deadline
	if reset {
		clear(s.values)
		clear(s.encoded)
		s.idle = s.config.IdleTimeout
	}
	return nil
}
