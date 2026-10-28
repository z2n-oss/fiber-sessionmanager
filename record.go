package sessionmanager

import (
	"bytes"
	"context"
	"strings"
	"time"
)

type record struct {
	Version   int               `cbor:"version" json:"version"`
	ExpiresAt int64             `cbor:"expires_at" json:"expires_at"`
	Values    map[string][]byte `cbor:"values" json:"values"`
}

func decodeRecord(n *namespaceConfig, raw []byte) (record, error) {
	var r record
	if err := n.Unmarshal(bytes.Clone(raw), &r); err != nil {
		return record{}, n.wrap("decode record", "", err)
	}
	if r.Version != 1 || r.Values == nil || r.ExpiresAt < 0 {
		return record{}, n.wrap("load", "", ErrInvalidRecord)
	}
	r.Values = cloneEncoded(r.Values)
	return r, nil
}

func cloneEncoded(values map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(values))
	for key, value := range values {
		cloned[strings.Clone(key)] = bytes.Clone(value)
	}
	return cloned
}

func loadSession(ctx context.Context, n *namespaceConfig, id string) (*Session, error) {
	if id == "" {
		return nil, n.wrap("load", "", ErrInvalidSessionID)
	}
	id = strings.Clone(id)
	raw, err := n.Storage.GetWithContext(nonNilContext(ctx), n.storageKey(id))
	if err != nil {
		return nil, n.wrap("load", "", err)
	}
	if raw == nil {
		return nil, nil
	}
	r, err := decodeRecord(n, raw)
	if err != nil {
		return nil, err
	}
	if r.ExpiresAt != 0 && !n.now().Before(time.Unix(0, r.ExpiresAt)) {
		return nil, nil
	}
	return &Session{
		config: n, id: id, idle: n.IdleTimeout, expiresAt: r.ExpiresAt,
		encoded: r.Values, values: make(map[string]any),
	}, nil
}

func absoluteDeadline(now time.Time, timeout time.Duration) (int64, error) {
	if timeout == 0 {
		return 0, nil
	}
	deadline := now.Add(timeout)
	nanos := deadline.UnixNano()
	if nanos <= 0 || !time.Unix(0, nanos).Equal(deadline) {
		return 0, ErrInvalidRecord
	}
	return nanos, nil
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
