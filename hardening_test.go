package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
)

func TestConfigurationHardening(t *testing.T) {
	var typedNil *testStorage
	for name, cfg := range map[string]Config{
		"typed nil storage":        {Storage: typedNil},
		"domain":                   {CookieDomain: "https://example.com"},
		"relative path":            {CookiePath: "account"},
		"path separator":           {CookiePath: "/account;secure"},
		"path newline":             {CookiePath: "/\n"},
		"cookie name":              {Extractor: extractors.FromCookie("bad name")},
		"empty cookie":             {Extractor: extractors.FromCookie("")},
		"header name":              {Extractor: extractors.FromHeader("X-Id\r\nInjected")},
		"secure prefix":            {Extractor: extractors.FromCookie("__Secure-id")},
		"host prefix secure":       {Extractor: extractors.FromCookie("__Host-id")},
		"host prefix domain":       {Extractor: extractors.FromCookie("__Host-id"), CookieSecure: true, CookieDomain: "example.com"},
		"host prefix path":         {Extractor: extractors.FromCookie("__Host-id"), CookieSecure: true, CookiePath: "/account"},
		"unrepresentable deadline": {AbsoluteTimeout: time.Duration(math.MaxInt64)},
		"metadata only":            {Extractor: extractors.Extractor{Source: extractors.SourceCookie, Key: "id"}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg.Namespace = "a"
			checkPanic(t, func() { New(Config{Namespace: "first"}, cfg) })
		})
	}
	for _, cfg := range []Config{
		{Namespace: "a"},
		{Namespace: "a", CookieSameSite: "none", Extractor: extractors.FromCookie("__Host-id")},
		{Namespace: "a", CookieSecure: true, CookieHTTPOnly: true, Extractor: extractors.FromCookie("__Secure-id")},
	} {
		m := managerFor(t, cfg)
		app := fiber.New()
		app.Use(m.Handler())
		app.Get("/nested/page", func(c fiber.Ctx) error { _, err := FromContext(c, "a"); return err })
		response := request(t, app, "/nested/page")
		cookies := response.Cookies()
		if len(cookies) != 1 {
			t.Fatal("cookie silently dropped")
		}
		cookie := cookies[0]
		if cookie.Path != "/" || cookie.HttpOnly != cfg.CookieHTTPOnly || cookie.Secure != (cfg.CookieSecure || cfg.CookieSameSite == "none") {
			t.Fatalf("cookie defaults: %+v", cookie)
		}
	}
}

func TestGeneratedIDValidationPrecedesDeletion(t *testing.T) {
	for _, replacement := range []string{"", "original", "bad;id", "bad\r\nid"} {
		t.Run(replacement, func(t *testing.T) {
			storage := storageFor(t)
			m := managerFor(t, Config{Namespace: "a", Storage: storage, KeyGenerator: func() string { return replacement }})
			seedSession(t, storage, "a", "original", 0, map[string]any{"stored": true})
			s, err := m.GetByID(nil, "a", "original")
			if err != nil {
				t.Fatal(err)
			}
			s.Set("pending", true)
			for _, rotate := range []func() error{s.Regenerate, s.Reset} {
				checkErr(t, rotate(), ErrInvalidSessionID)
				if s.ID() != "original" || s.Fresh() || storage.deletes != 0 || !reflect.DeepEqual(s.Keys(), []string{"pending", "stored"}) {
					t.Fatal("invalid rotation modified the old session")
				}
			}
		})
	}
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage, KeyGenerator: func() string { panic("random source") }})
	seedSession(t, storage, "a", "original", 0, nil)
	s, err := m.GetByID(nil, "a", "original")
	if err != nil {
		t.Fatal(err)
	}
	checkPanic(t, func() { _ = s.Regenerate() })
	if storage.deletes != 0 || s.ID() != "original" {
		t.Fatal("generator panic deleted the old session")
	}
}

func TestInvalidNewIDDoesNotInstallSession(t *testing.T) {
	for _, tc := range []struct {
		id, supplied string
		extractor    extractors.Extractor
	}{
		{id: ""},
		{id: "attacker", supplied: "attacker"},
		{id: "bad;cookie"},
		{id: "bad\nheader", extractor: extractors.FromHeader("X-ID")},
	} {
		t.Run(tc.id, func(t *testing.T) {
			storage := storageFor(t)
			m := managerFor(t, Config{Namespace: "a", Storage: storage, KeyGenerator: func() string { return tc.id }, Extractor: tc.extractor})
			app := fiber.New()
			app.Use(m.Handler())
			app.Get("/", func(c fiber.Ctx) error {
				s, err := FromContext(c, "a")
				checkErr(t, err, ErrInvalidSessionID)
				if s != nil {
					t.Error("invalid ID returned a session")
				}
				return nil
			})
			response := request(t, app, "/", &http.Cookie{Name: "session_id_a", Value: tc.supplied})
			if storage.sets != 0 || len(response.Cookies()) != 0 || response.Header.Get("X-ID") != "" {
				t.Fatal("invalid ID was persisted or emitted")
			}
		})
	}
}

func TestCodecMissingSentinelIsNotAStorageMiss(t *testing.T) {
	storage := storageFor(t)
	seedSession(t, storage, "a", "existing", 0, nil)
	m := managerFor(t, Config{Namespace: "a", Storage: storage, Marshal: cbor.Marshal,
		Unmarshal: func([]byte, any) error { return ErrSessionNotFound },
	})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := FromContext(c, "a")
		checkErr(t, err, ErrSessionNotFound)
		if s != nil {
			t.Error("Codec failure treated as a storage miss")
		}
		return nil
	})
	response := request(t, app, "/", &http.Cookie{Name: "session_id_a", Value: "existing"})
	if storage.sets != 0 || len(response.Cookies()) != 0 {
		t.Fatal("failed load installed a replacement")
	}
}

func TestEmptyEncodedEnvelopeDoesNotWrite(t *testing.T) {
	storage := storageFor(t)
	handled := 0
	m := managerFor(t, Config{Namespace: "a", Storage: storage,
		Marshal: func(value any) ([]byte, error) {
			if _, ok := value.(record); ok {
				return nil, nil
			}
			return cbor.Marshal(value)
		}, Unmarshal: cbor.Unmarshal,
		ErrorHandler: func(c fiber.Ctx, err error) { handled++; checkErr(t, err, ErrInvalidRecord) },
	})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := FromContext(c, "a")
		if err != nil {
			return err
		}
		s.Set("value", 1)
		checkErr(t, s.Save(), ErrInvalidRecord)
		return nil
	})
	response := request(t, app, "/")
	if handled != 1 || storage.sets != 0 || len(response.Cookies()) != 0 {
		t.Fatal("empty record reached storage or emitted an identifier")
	}
}

// This provider deliberately retains write buffers and returns borrowed reads.
type borrowingStorage struct {
	fiber.Storage
	data map[string][]byte
}

func (s *borrowingStorage) GetWithContext(_ context.Context, key string) ([]byte, error) {
	return s.data[key], nil
}

func (s *borrowingStorage) SetWithContext(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.data[key] = value
	return nil
}

func TestMarshalBufferOwnership(t *testing.T) {
	storage := &borrowingStorage{data: make(map[string][]byte)}
	buffer := make([]byte, 0, 4096)
	marshal := func(value any) ([]byte, error) {
		raw, err := cbor.Marshal(value)
		buffer = append(buffer[:0], raw...)
		return buffer, err
	}
	m := managerFor(t, Config{Namespace: "a", Storage: storage, Marshal: marshal, Unmarshal: cbor.Unmarshal})
	s, err := newSession(m.namespaces["a"], "")
	if err != nil {
		t.Fatal(err)
	}
	s.Set("alpha", "first")
	s.Set("beta", "second")
	checkErr(t, s.Save(), nil)
	_, err = marshal(strings.Repeat("overwrite", 64))
	checkErr(t, err, nil)
	loaded, err := m.GetByID(nil, "a", s.ID())
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, loaded, "alpha", "first", false)
	roundTrip(t, loaded, "beta", "second", false)
	clear(storage.data[s.storageKey(s.ID())])
	roundTrip(t, loaded, "alpha", "first", false)
}

func TestUnmarshalInputAndResultOwnership(t *testing.T) {
	for _, mutateInput := range []bool{false, true} {
		t.Run(map[bool]string{false: "alias result", true: "mutate input"}[mutateInput], func(t *testing.T) {
			m := managerFor(t, Config{Namespace: "a",
				Marshal: func(value any) ([]byte, error) {
					if raw, ok := value.([]byte); ok {
						return raw, nil
					}
					return cbor.Marshal(value)
				},
				Unmarshal: func(raw []byte, target any) error {
					if value, ok := target.(*[]byte); ok {
						if mutateInput {
							*value = bytes.Clone(raw)
							clear(raw)
						} else {
							*value = raw
						}
						return nil
					}
					return cbor.Unmarshal(raw, target)
				},
			})
			s, err := newSession(m.namespaces["a"], "")
			if err != nil {
				t.Fatal(err)
			}
			original := []byte{1, 2, 3}
			s.Set("value", original)
			for pass := range 2 {
				var target []byte
				checkErr(t, s.Get("value", &target), nil)
				target[0] = 9
				roundTrip(t, s, "value", []byte{1, 2, 3}, false)
				if !bytes.Equal(original, []byte{1, 2, 3}) {
					t.Fatal("decoder modified the pending application value")
				}
				if pass == 0 {
					checkErr(t, s.Save(), nil)
					s, err = m.GetByID(nil, "a", s.ID())
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestJSONCodec(t *testing.T) {
	m := managerFor(t, Config{Namespace: "a", Marshal: json.Marshal, Unmarshal: json.Unmarshal})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := FromContext(c, "a")
		if err != nil {
			return err
		}
		roundTrip(t, s, "nil", []string(nil), s.Fresh())
		roundTrip(t, s, "empty", []string{}, s.Fresh())
		roundTrip(t, s, "map", map[string]int{"answer": 42}, s.Fresh())
		return nil
	})
	response := request(t, app, "/")
	request(t, app, "/", response.Cookies()...)
}

func TestFailedSavePreservesPendingStateAndResponse(t *testing.T) {
	storage := storageFor(t)
	storage.setErr = errStorage
	m := managerFor(t, Config{Namespace: "a", Storage: storage})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := FromContext(c, "a")
		if err != nil {
			return err
		}
		s.Set("value", 1)
		checkErr(t, s.Save(), errStorage)
		if len(c.Response().Header.Peek("Set-Cookie")) != 0 {
			t.Error("failed save emitted a cookie")
		}
		roundTrip(t, s, "value", 1, false)
		c.Set(fiber.HeaderContentEncoding, "gzip")
		c.Set(fiber.HeaderETag, "stale")
		c.Set(fiber.HeaderLocation, "/success")
		return c.JSON(fiber.Map{"ok": true})
	})
	response := request(t, app, "/")
	body, err := io.ReadAll(response.Body)
	checkErr(t, err, nil)
	if response.StatusCode != 500 || string(body) != "Internal Server Error" || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") {
		t.Fatal("invalid failure response")
	}
	for _, header := range []string{"Content-Encoding", "ETag", "Location", "Set-Cookie"} {
		if response.Header.Get(header) != "" {
			t.Errorf("stale %s", header)
		}
	}
}

type failingCloseStorage struct {
	fiber.Storage
	err   error
	calls int
}

func (s *failingCloseStorage) Close() error { s.calls++; return s.err }

func TestClosePreservesAllErrorsAndIsConcurrent(t *testing.T) {
	external := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: external})
	first, second := errors.New("first close"), errors.New("second close")
	a, b := &failingCloseStorage{err: first}, &failingCloseStorage{err: second}
	m.owned = []fiber.Storage{a, b}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			err := m.Close()
			if !errors.Is(err, first) || !errors.Is(err, second) {
				t.Errorf("close errors lost: %v", err)
			}
		})
	}
	wg.Wait()
	if a.calls != 1 || b.calls != 1 || external.closes != 0 {
		t.Fatal("storage ownership or close idempotency")
	}
}
