package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/storage/memory/v2"
)

// Manager manages independent namespaces and supports concurrent requests and
// background operations. Drain all work before calling Close.
type Manager struct {
	configs    []namespaceConfig
	namespaces map[string]*namespaceConfig
	owned      []fiber.Storage
	closeOnce  sync.Once
	closeErr   error
}

type contextKey struct{}

type container struct {
	manager  *Manager
	sessions map[string]*Session
	replaced map[string]string
}

// New validates all configurations before allocating default storage.
// Invalid configuration panics. At least one named namespace is required.
func New(configs ...Config) *Manager {
	if len(configs) == 0 {
		panic("sessionmanager: at least one namespace is required")
	}
	m := &Manager{
		configs:    make([]namespaceConfig, len(configs)),
		namespaces: make(map[string]*namespaceConfig, len(configs)),
	}
	locations := make(map[identifierSink]string)
	for i, config := range configs {
		n := normalize(config)
		if _, ok := m.namespaces[n.Namespace]; ok {
			panic("sessionmanager: duplicate namespace " + n.Namespace)
		}
		for _, sink := range n.sinks {
			location := sink.identity()
			if owner, ok := locations[location]; ok && owner != n.Namespace {
				panic("sessionmanager: conflicting identifier location " + sink.key)
			}
			locations[location] = n.Namespace
		}
		m.configs[i] = n
		m.namespaces[n.Namespace] = &m.configs[i]
	}
	for i := range m.configs {
		if m.configs[i].Storage == nil {
			storage := memory.New()
			m.configs[i].Storage = storage
			m.owned = append(m.owned, storage)
		}
	}
	return m
}

// Close closes all manager-owned stores, preserving any errors with errors.Join.
// Supplied stores remain caller-owned. Repeated calls return the same result.
// Manager and Session use after Close is unsupported.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		for _, storage := range m.owned {
			if err := storage.Close(); err != nil {
				m.closeErr = errors.Join(m.closeErr, err)
			}
		}
	})
	return m.closeErr
}

// Handler installs a request container and saves accessed sessions in registration
// order after normal return, including business errors. Propagating panics skip
// subsequent implicit saves. Only the outermost invocation of this Manager saves.
func (m *Manager) Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		if current, _ := c.Locals(contextKey{}).(*container); current != nil {
			if current.manager != m {
				return ErrManagerConflict
			}
			return c.Next()
		}
		state := &container{
			manager: m, sessions: make(map[string]*Session), replaced: make(map[string]string),
		}
		c.Locals(contextKey{}, state)
		defer func() {
			c.Locals(contextKey{}, nil)
			clear(state.sessions)
			clear(state.replaced)
		}()
		err := c.Next()
		for i := range m.configs {
			config := &m.configs[i]
			if s := state.sessions[config.Namespace]; s != nil {
				if saveErr := s.Save(); saveErr != nil {
					config.ErrorHandler(c, saveErr)
				}
			}
		}
		return err
	}
}

func (m *Manager) namespace(name string) (*namespaceConfig, error) {
	if config := m.namespaces[name]; config != nil {
		return config, nil
	}
	return nil, fmt.Errorf("session namespace %q: %w", name, ErrUnknownNamespace)
}

// GetByID loads an existing session without an HTTP binding. Missing or absolutely
// expired sessions return ErrSessionNotFound. Reads do not renew expiry or save.
// The loading context is not retained: use the Session's WithContext methods for
// later operations. No-argument detached operations use context.Background().
func (m *Manager) GetByID(ctx context.Context, namespace, id string) (*Session, error) {
	n, err := m.namespace(namespace)
	if err != nil {
		return nil, err
	}
	s, err := loadSession(ctx, n, id)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, n.wrap("load", "", ErrSessionNotFound)
	}
	return s, nil
}

// DeleteByID deletes a record without decoding it. Missing records are harmless;
// empty IDs return ErrInvalidSessionID. This does not invalidate loaded snapshots
// or prevent concurrent saves from recreating the record. Within a request, use
// Session.Destroy to also remove the object from automatic saving.
func (m *Manager) DeleteByID(ctx context.Context, namespace, id string) error {
	n, err := m.namespace(namespace)
	if err != nil {
		return err
	}
	if id == "" {
		return n.wrap("delete", "", ErrInvalidSessionID)
	}
	if err := n.Storage.DeleteWithContext(nonNilContext(ctx), n.storageKey(id)); err != nil {
		return n.wrap("delete", "", err)
	}
	return nil
}

// FromContext lazily loads a namespace, returning the same object within a request.
// Storage misses and absolute expiry create a fresh ID, never adopting an unknown ID.
func FromContext(c fiber.Ctx, namespace string) (*Session, error) {
	return FromContextWithContext(c.Context(), c, namespace)
}

// FromContextWithContext uses ctx for the first storage read only. An already
// loaded session is reused. A nil ctx selects context.Background().
func FromContextWithContext(ctx context.Context, c fiber.Ctx, namespace string) (*Session, error) {
	state, _ := c.Locals(contextKey{}).(*container)
	if state == nil {
		return nil, ErrMiddlewareMissing
	}
	config, err := state.manager.namespace(namespace)
	if err != nil {
		return nil, err
	}
	if s := state.sessions[namespace]; s != nil {
		return s, nil
	}
	binding := &requestBinding{ctx: c, owner: state}
	previous, replaced := state.replaced[namespace]
	var s *Session
	if !replaced {
		result, err := extractors.Resolve(config.Extractor, c)
		if err != nil && !errors.Is(err, extractors.ErrNotFound) {
			return nil, config.wrap("extract", "", err)
		}
		if err == nil {
			result.Value, result.Key = strings.Clone(result.Value), strings.Clone(result.Key)
			binding.source = result
			previous = result.Value
			s, err = loadSession(ctx, config, result.Value)
			if err != nil {
				return nil, err
			}
		}
	}
	if s == nil {
		s, err = newSession(config, previous)
		if err != nil {
			return nil, err
		}
	}
	s.request = binding
	state.sessions[namespace] = s
	return s, nil
}
