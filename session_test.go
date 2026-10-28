package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/storage/memory/v2"
)

var errStorage = errors.New("storage unavailable")

type testStorage struct {
	fiber.Storage
	gets, sets, deletes, closes int
	getErr, setErr, deleteErr   error
	ttl                         time.Duration
	seenContext                 context.Context
}

func (s *testStorage) GetWithContext(ctx context.Context, key string) ([]byte, error) {
	s.gets++
	s.seenContext = ctx
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.Storage.GetWithContext(ctx, key)
}
func (s *testStorage) SetWithContext(ctx context.Context, key string, raw []byte, ttl time.Duration) error {
	s.sets++
	s.ttl = ttl
	s.seenContext = ctx
	if s.setErr != nil {
		return s.setErr
	}
	return s.Storage.SetWithContext(ctx, key, raw, ttl)
}
func (s *testStorage) DeleteWithContext(ctx context.Context, key string) error {
	s.deletes++
	s.seenContext = ctx
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.Storage.DeleteWithContext(ctx, key)
}
func (s *testStorage) Close() error { s.closes++; return s.Storage.Close() }
func storageFor(t *testing.T) *testStorage {
	t.Helper()
	s := &testStorage{Storage: memory.New()}
	t.Cleanup(func() { _ = s.Storage.Close() })
	return s
}
func managerFor(t *testing.T, configs ...Config) *Manager {
	t.Helper()
	m := New(configs...)
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func request(t *testing.T, app *fiber.App, url string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}
func mustSession(t *testing.T, c fiber.Ctx, ns string) *Session {
	t.Helper()
	s, err := FromContext(c, ns)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func checkErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
func checkPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected panic")
		}
	}()
	fn()
}

func TestConfiguration(t *testing.T) {
	for name, configs := range map[string][]Config{
		"empty": nil, "namespace": {{Namespace: "bad.name"}}, "missing namespace": {{}},
		"duplicate": {{Namespace: "a"}, {Namespace: "a"}}, "idle": {{Namespace: "a", IdleTimeout: -1}},
		"absolute": {{Namespace: "a", AbsoluteTimeout: -1}}, "short absolute": {{Namespace: "a", AbsoluteTimeout: time.Second}},
		"codec": {{Namespace: "a", Marshal: cbor.Marshal}}, "same site": {{Namespace: "a", CookieSameSite: "invalid"}},
		"cookie collision": {{Namespace: "a", Extractor: extractors.FromCookie("id")}, {Namespace: "b", Extractor: extractors.FromCookie("id")}},
		"header collision": {{Namespace: "a", Extractor: extractors.FromHeader("X-Id")}, {Namespace: "b", Extractor: extractors.Chain(extractors.FromQuery("q"), extractors.FromHeader("x-id"))}},
	} {
		t.Run(name, func(t *testing.T) { checkPanic(t, func() { New(configs...) }) })
	}
	external := storageFor(t)
	m := managerFor(t, Config{Namespace: "a"}, Config{Namespace: "b", Storage: external})
	a := m.namespaces["a"]
	if a.IdleTimeout != 30*time.Minute || a.AbsoluteTimeout != 0 || a.CookieSameSite != "lax" || a.Extractor.Key != "session_id_a" || len(m.owned) != 1 {
		t.Fatal("incorrect defaults")
	}
	checkErr(t, m.Close(), nil)
	checkErr(t, m.Close(), nil)
	if external.closes != 0 {
		t.Fatal("closed caller-owned storage")
	}
}

func TestContainerAndLazyLoading(t *testing.T) {
	a, b := storageFor(t), storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: a}, Config{Namespace: "b", Storage: b})
	other := managerFor(t, Config{Namespace: "other"})
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		_, err := FromContext(c, "unknown")
		checkErr(t, err, ErrMiddlewareMissing)
		err = c.Next()
		if c.Locals(contextKey{}) != nil {
			t.Error("container not cleaned")
		}
		return err
	})
	app.Use(m.Handler(), m.Handler())
	app.Get("/unused", func(c fiber.Ctx) error { return nil })
	app.Get("/used", func(c fiber.Ctx) error {
		_, err := FromContext(c, "unknown")
		checkErr(t, err, ErrUnknownNamespace)
		s := mustSession(t, c, "a")
		if mustSession(t, c, "a") != s {
			t.Error("not reused")
		}
		s.Set("a", 1)
		checkErr(t, other.Handler()(c), ErrManagerConflict)
		if mustSession(t, c, "a") != s {
			t.Error("conflict replaced container")
		}
		return nil
	})
	if len(request(t, app, "/unused").Cookies()) != 0 {
		t.Error("unused cookie")
	}
	response := request(t, app, "/used")
	if a.gets != 0 || a.sets != 1 || b.gets != 0 || b.sets != 0 || len(response.Cookies()) != 1 {
		t.Fatal("lazy behavior or reentry failed")
	}
	request(t, app, "/used", response.Cookies()...)
	if a.gets != 1 || a.sets != 2 {
		t.Fatal("load/save count")
	}
}

type customValue string

func (v customValue) MarshalCBOR() ([]byte, error) { return cbor.Marshal("custom:" + string(v)) }
func (v *customValue) UnmarshalCBOR(raw []byte) error {
	var value string
	if err := cbor.Unmarshal(raw, &value); err != nil {
		return err
	}
	*v = customValue(strings.TrimPrefix(value, "custom:"))
	return nil
}
func roundTrip[T any](t *testing.T, s *Session, key string, want T, set bool) {
	t.Helper()
	if set {
		s.Set(key, want)
	}
	var got T
	err := s.Get(key, &got)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v, err %v", key, got, want, err)
	}
}
func TestCodecRoundTripAndReferences(t *testing.T) {
	m := managerFor(t, Config{Namespace: "a"})
	app := fiber.New()
	app.Use(m.Handler())
	type profile struct {
		Name  string
		Roles []string
	}
	p := &profile{Name: "Ada", Roles: []string{}}
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		first := s.Fresh()
		roundTrip(t, s, "nil slice", []string(nil), first)
		roundTrip(t, s, "empty slice", []string{}, first)
		roundTrip(t, s, "nil map", map[string]int(nil), first)
		roundTrip(t, s, "empty map", map[string]int{}, first)
		roundTrip(t, s, "nil pointer", (*profile)(nil), first)
		roundTrip(t, s, "struct", *p, first)
		roundTrip(t, s, "pointer", p, first)
		roundTrip(t, s, "custom", customValue("value"), first)
		if first {
			values := []int{1}
			s.Set("reference", values)
			checkErr(t, s.Save(), nil)
			values[0] = 2
			roundTrip(t, s, "reference", []int{2}, false)
		} else {
			var values []int
			err := s.Get("reference", &values)
			checkErr(t, err, nil)
			values[0] = 3
			roundTrip(t, s, "reference", []int{2}, false)
		}
		missing := 42
		err := s.Get("missing", &missing)
		checkErr(t, err, ErrKeyNotFound)
		if missing != 42 {
			t.Error("missing key changed target")
		}
		s.Set("delete", 1)
		s.Delete("delete")
		err = s.Get("delete", &missing)
		checkErr(t, err, ErrKeyNotFound)
		if len(s.Keys()) != 9 {
			t.Errorf("keys %v", s.Keys())
		}
		return nil
	})
	cookies := request(t, app, "/").Cookies()
	request(t, app, "/", cookies...)
}

func TestLazyCodecAndFailureAtomicity(t *testing.T) {
	storage := storageFor(t)
	marshals, unmarshals := 0, 0
	fail := false
	m := managerFor(t, Config{Namespace: "a", Storage: storage,
		Marshal: func(v any) ([]byte, error) {
			marshals++
			if fail {
				return nil, errStorage
			}
			return cbor.Marshal(v)
		},
		Unmarshal: func(raw []byte, v any) error { unmarshals++; return cbor.Unmarshal(raw, v) },
	})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		if s.Fresh() {
			s.Set("a", 1)
			s.Set("b", 2)
			if marshals != 0 || unmarshals != 0 {
				t.Error("Set called codec")
			}
			fail = true
			checkErr(t, s.Save(), errStorage)
			if storage.sets != 0 || len(c.Response().Header.Peek("Set-Cookie")) != 0 {
				t.Error("failed encoding had side effects")
			}
			fail = false
		} else {
			if unmarshals != 1 {
				t.Errorf("load decoded values: %d", unmarshals)
			}
			roundTrip(t, s, "a", 1, false)
			if unmarshals != 2 {
				t.Error("Get did not decode just one key")
			}
			s.Delete("b")
		}
		return nil
	})
	cookies := request(t, app, "/").Cookies()
	marshals, unmarshals = 0, 0
	request(t, app, "/", cookies...)
	if marshals != 1 {
		t.Errorf("encoded unchanged values: %d", marshals)
	}
}

func TestLifecycle(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage, AbsoluteTimeout: time.Hour})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		s.Set("value", 1)
		checkErr(t, s.Save(), nil)
		old, deadline := s.ID(), s.expiresAt
		storage.deleteErr = errStorage
		for _, op := range []func() error{s.Destroy, s.Regenerate, s.Reset} {
			checkErr(t, op(), errStorage)
			if s.ID() != old || s.expiresAt != deadline {
				t.Error("failed delete changed state")
			}
			roundTrip(t, s, "value", 1, false)
		}
		storage.deleteErr = nil
		checkErr(t, s.Regenerate(), nil)
		if s.ID() == old || s.expiresAt != deadline || !s.Fresh() {
			t.Error("regenerate")
		}
		raw, err := storage.Get(s.storageKey(old))
		checkErr(t, err, nil)
		if raw != nil {
			t.Error("old ID remains")
		}
		roundTrip(t, s, "value", 1, false)
		s.SetIdleTimeout(time.Minute)
		old = s.ID()
		checkErr(t, s.Reset(), nil)
		if s.ID() == old || len(s.Keys()) != 0 || s.idle != 30*time.Minute || s.expiresAt < deadline {
			t.Error("reset")
		}
		checkErr(t, s.Save(), nil)
		old = s.ID()
		checkErr(t, s.Destroy(), nil)
		fresh := mustSession(t, c, "a")
		if fresh == s || fresh.ID() == old || !fresh.Fresh() || len(fresh.Keys()) != 0 {
			t.Error("destroy/recreate")
		}
		return nil
	})
	response := request(t, app, "/")
	if len(response.Cookies()) != 1 || response.Cookies()[0].MaxAge <= 0 {
		t.Error("replacement cookie")
	}
	// Rebuilding must ignore the incoming ID, even when Destroy happened on a loaded session.
	request(t, app, "/", response.Cookies()...)
}

func TestErrorsAndSaveOrder(t *testing.T) {
	a, b := storageFor(t), storageFor(t)
	a.setErr, b.setErr = errStorage, errStorage
	var order []string
	businessErr := errors.New("business")
	m := managerFor(t,
		Config{Namespace: "a", Storage: a, ErrorHandler: func(c fiber.Ctx, err error) { checkErr(t, err, errStorage); order = append(order, "a") }},
		Config{Namespace: "b", Storage: b, ErrorHandler: func(c fiber.Ctx, err error) { checkErr(t, err, errStorage); order = append(order, "b") }},
	)
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		if err != businessErr {
			t.Error("business error replaced")
		}
		return c.SendStatus(418)
	}})
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		mustSession(t, c, "b")
		s := mustSession(t, c, "a")
		checkErr(t, s.Save(), errStorage)
		if len(order) != 0 {
			t.Error("explicit error dispatched")
		}
		return businessErr
	})
	response := request(t, app, "/")
	if response.StatusCode != 418 || !reflect.DeepEqual(order, []string{"a", "b"}) {
		t.Fatalf("order/status: %v %d", order, response.StatusCode)
	}
	app = fiber.New()
	app.Use(managerFor(t, Config{Namespace: "a", Storage: a}).Handler())
	app.Get("/", func(c fiber.Ctx) error { mustSession(t, c, "a"); return c.SendString("ok") })
	response = request(t, app, "/")
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 500 || string(body) != "Internal Server Error" {
		t.Error("default error handler")
	}
}

func TestPanicCleanup(t *testing.T) {
	for _, kind := range []string{"business", "codec", "error handler"} {
		t.Run(kind, func(t *testing.T) {
			a, b := storageFor(t), storageFor(t)
			config := Config{Namespace: "a", Storage: a}
			if kind == "codec" {
				config.Marshal = func(any) ([]byte, error) { panic("codec") }
				config.Unmarshal = cbor.Unmarshal
			}
			if kind == "error handler" {
				a.setErr = errStorage
				config.ErrorHandler = func(fiber.Ctx, error) { panic("handler") }
			}
			m := managerFor(t, config, Config{Namespace: "b", Storage: b})
			app := fiber.New()
			app.Use(func(c fiber.Ctx) (err error) {
				defer func() {
					if recover() == nil {
						t.Error("panic not propagated")
					}
					if c.Locals(contextKey{}) != nil {
						t.Error("container leaked")
					}
					err = c.SendStatus(204)
				}()
				return c.Next()
			})
			app.Use(m.Handler())
			app.Get("/", func(c fiber.Ctx) error {
				mustSession(t, c, "a")
				mustSession(t, c, "b")
				if kind == "business" {
					panic("business")
				}
				return nil
			})
			request(t, app, "/")
			if b.sets != 0 || (kind == "business" && a.sets != 0) {
				t.Error("panic committed sessions")
			}
		})
	}
}

func TestLoadErrorsAndContext(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage})
	app := fiber.New()
	app.Use(m.Handler())
	expected := errStorage
	ctx := context.WithValue(t.Context(), struct{}{}, "marker")
	app.Get("/", func(c fiber.Ctx) error {
		_, err := FromContextWithContext(ctx, c, "a")
		checkErr(t, err, expected)
		if storage.seenContext != ctx {
			t.Error("context not forwarded")
		}
		return nil
	})
	cookie := &http.Cookie{Name: "session_id_a", Value: "secret-id"}
	storage.getErr = errStorage
	request(t, app, "/", cookie)
	storage.getErr = nil
	for _, raw := range [][]byte{[]byte("corrupt"), mustEncode(t, record{Version: 2, Values: map[string][]byte{}}), mustEncode(t, record{Version: 1})} {
		checkErr(t, storage.Set("session:a:secret-id", raw, time.Minute), nil)
		if string(raw) == "corrupt" {
			// Codec errors preserve their concrete type, rather than replacing them with a sentinel.
			expected = nil
			app2 := fiber.New()
			app2.Use(m.Handler())
			app2.Get("/", func(c fiber.Ctx) error {
				_, err := FromContext(c, "a")
				if err == nil || strings.Contains(err.Error(), "secret-id") {
					t.Errorf("load error: %v", err)
				}
				return nil
			})
			request(t, app2, "/", cookie)
		} else {
			expected = ErrInvalidRecord
			request(t, app, "/", cookie)
		}
	}
	if storage.sets != 0 {
		t.Error("failed loads auto-saved")
	}
}
func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := cbor.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExpiryAndCookie(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage, IdleTimeout: time.Minute, AbsoluteTimeout: time.Hour,
		CookiePath: "/", CookieDomain: "example.com", CookieHTTPOnly: true, CookieSameSite: "NoNe"})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		s.expiresAt = time.Now().Add(20 * time.Second).UnixNano()
		checkErr(t, s.Save(), nil)
		if storage.ttl <= 0 || storage.ttl > 20*time.Second {
			t.Error("absolute TTL")
		}
		s.expiresAt = time.Now().Add(-time.Second).UnixNano()
		before := storage.sets
		checkErr(t, s.Save(), ErrExpired)
		if storage.sets != before {
			t.Error("expired session was written")
		}
		// Avoid automatic expiry response after testing explicit failure.
		return s.Destroy()
	})
	response := request(t, app, "/")
	cookie := response.Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteNoneMode || cookie.Path != "/" || cookie.Domain != "example.com" || cookie.MaxAge != -1 {
		t.Errorf("cookie: %+v", cookie)
	}
	// Both expired absolute records and storage misses replace the client-supplied ID.
	for _, absolute := range []int64{time.Now().Add(-time.Hour).UnixNano(), 0} {
		if absolute != 0 {
			checkErr(t, storage.Set("session:a:old", mustEncode(t, record{Version: 1, ExpiresAt: absolute, Values: map[string][]byte{}}), time.Hour), nil)
		} else {
			checkErr(t, storage.Delete("session:a:old"), nil)
		}
		app2 := fiber.New()
		app2.Use(m.Handler())
		app2.Get("/", func(c fiber.Ctx) error {
			s := mustSession(t, c, "a")
			if s.ID() == "old" || !s.Fresh() {
				t.Error("expired/missing not replaced")
			}
			return nil
		})
		request(t, app2, "/", &http.Cookie{Name: "session_id_a", Value: "old"})
	}
}

func TestExtractorProvenance(t *testing.T) {
	for _, source := range []string{"cookie", "header", "query", "unresolved", "missing"} {
		t.Run(source, func(t *testing.T) {
			storage := storageFor(t)
			ext := extractors.Chain(extractors.FromCookie("id"), extractors.Chain(extractors.FromHeader("X-Id"), extractors.FromQuery("id")))
			if source == "unresolved" {
				ext.Extract = func(fiber.Ctx) (string, error) { return "existing", nil }
			}
			checkErr(t, storage.Set("session:a:existing", mustEncode(t, record{Version: 1, Values: map[string][]byte{}}), time.Hour), nil)
			m := managerFor(t, Config{Namespace: "a", Storage: storage, Extractor: ext, CookieSessionOnly: true})
			app := fiber.New()
			app.Use(m.Handler())
			app.Get("/", func(c fiber.Ctx) error { mustSession(t, c, "a"); return nil })
			req := httptest.NewRequest("GET", "/", nil)
			switch source {
			case "cookie":
				req.AddCookie(&http.Cookie{Name: "id", Value: "existing"})
			case "header":
				req.Header.Set("X-Id", "existing")
			case "query":
				req = httptest.NewRequest("GET", "/?id=existing", nil)
			}
			response, err := app.Test(req)
			checkErr(t, err, nil)
			defer response.Body.Close()
			cookies, header := response.Cookies(), response.Header.Get("X-Id")
			switch source {
			case "cookie":
				if len(cookies) != 1 || header != "" {
					t.Error("wrong cookie provenance")
				}
			case "header":
				if len(cookies) != 0 || header != "existing" {
					t.Error("wrong header provenance")
				}
			case "query", "unresolved":
				if len(cookies) != 0 || header != "" {
					t.Error("read-only ID promoted")
				}
			case "missing":
				if len(cookies) != 1 || header == "" {
					t.Error("fresh ID not written")
				}
			}
			for _, cookie := range cookies {
				if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
					t.Error("session-only cookie has expiry")
				}
			}
		})
	}
}

func TestConcurrentRequests(t *testing.T) {
	m := managerFor(t, Config{Namespace: "a"})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/:value", func(c fiber.Ctx) error {
		s, err := FromContext(c, "a")
		if err != nil {
			return err
		}
		s.Set("value", strings.Clone(c.Params("value")))
		var value string
		if err := s.Get("value", &value); err != nil {
			return err
		}
		return c.SendString(value)
	})
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			value := fmt.Sprint(i)
			response, err := app.Test(httptest.NewRequest("GET", "/"+value, nil))
			if err != nil {
				t.Error(err)
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != value {
				t.Errorf("isolation: %s %v", body, err)
			}
		})
	}
	wg.Wait()
}

func TestNamespaceIsolationAndContextOperations(t *testing.T) {
	storage := storageFor(t)
	generator := func() func() string {
		var sequence int
		return func() string { sequence++; return fmt.Sprintf("shared-%d", sequence) }
	}
	m := managerFor(t, Config{Namespace: "a", Storage: storage, KeyGenerator: generator()}, Config{Namespace: "b", Storage: storage, KeyGenerator: generator()})
	ids := make(map[string]string)
	app := fiber.New()
	app.Use(m.Handler())
	ctx := context.WithValue(t.Context(), struct{}{}, "operations")
	app.Get("/", func(c fiber.Ctx) error {
		a, b := mustSession(t, c, "a"), mustSession(t, c, "b")
		if a.Fresh() {
			a.Set("value", "a")
			b.Set("value", "b")
		}
		roundTrip(t, a, "value", "a", false)
		roundTrip(t, b, "value", "b", false)
		for _, op := range []func(context.Context) error{a.SaveWithContext, a.RegenerateWithContext, a.ResetWithContext, a.DestroyWithContext} {
			checkErr(t, op(ctx), nil)
			if storage.seenContext != ctx {
				t.Error("operation context not forwarded")
			}
		}
		mustSession(t, c, "a").Set("value", "a")
		ids["a"], ids["b"] = mustSession(t, c, "a").ID(), b.ID()
		return nil
	})
	response := request(t, app, "/")
	request(t, app, "/", response.Cookies()...)
	for _, ns := range []string{"a", "b"} {
		raw, err := storage.Get("session:" + ns + ":" + ids[ns])
		checkErr(t, err, nil)
		if len(raw) == 0 {
			t.Error("namespace record missing")
		}
	}
}

func TestRequestCrossesAbsoluteDeadline(t *testing.T) {
	storage := storageFor(t)
	handled := 0
	now := time.Now()
	m := managerFor(t, Config{Namespace: "a", Storage: storage, IdleTimeout: time.Minute, AbsoluteTimeout: 2 * time.Minute,
		ErrorHandler: func(c fiber.Ctx, err error) { handled++; checkErr(t, err, ErrExpired) },
	})
	m.namespaces["a"].now = func() time.Time { return now }
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		s.Set("value", 1)
		now = now.Add(3 * time.Minute)
		return nil
	})
	response := request(t, app, "/")
	if handled != 1 || storage.sets != 0 || len(response.Cookies()) != 0 {
		t.Error("long request persisted expired data")
	}
}

func TestIdleExpiryAndTimeoutOverride(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		if !s.Fresh() {
			t.Error("idle-expired ID reused")
		}
		checkPanic(t, func() { s.SetIdleTimeout(0) })
		checkPanic(t, func() { s.SetIdleTimeout(-time.Second) })
		s.SetIdleTimeout(time.Minute)
		return nil
	})
	response := request(t, app, "/")
	if storage.ttl != time.Minute || response.Cookies()[0].MaxAge != 60 {
		t.Error("idle timeout not applied")
	}
	cookie := response.Cookies()[0]
	key := "session:a:" + cookie.Value
	raw, err := storage.Get(key)
	checkErr(t, err, nil)
	// memory/v2 uses second precision: a positive subsecond TTL expires in the current second.
	checkErr(t, storage.Set(key, raw, time.Nanosecond), nil)
	next := request(t, app, "/", cookie)
	if next.Cookies()[0].Value == cookie.Value {
		t.Error("idle expiry retained ID")
	}
}

func TestGetErrorsPreserveCodecBehaviorAndCause(t *testing.T) {
	cause := &cbor.SyntaxError{}
	failDecode := false
	var target *int
	m := managerFor(t, Config{Namespace: "a", Marshal: cbor.Marshal, Unmarshal: func(raw []byte, value any) error {
		if failDecode {
			if value != target {
				t.Error("codec did not receive the original target")
			}
			if p, ok := value.(*int); ok {
				*p = 99
			}
			return cause
		}
		return cbor.Unmarshal(raw, value)
	}})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		s.Set("bad", make(chan int))
		value := 42
		err := s.Get("bad", &value)
		if err == nil || value != 42 {
			t.Error("marshal failure")
		}
		s.Delete("bad")
		s.Set("value", 1)
		failDecode = true
		target = &value
		err = s.Get("value", target)
		var typed *cbor.SyntaxError
		if value != 99 || !errors.Is(err, cause) || !errors.As(err, &typed) {
			t.Errorf("decode failure: %d %v", value, err)
		}
		failDecode = false
		return nil
	})
	request(t, app, "/")
}

func TestGetInvalidTargets(t *testing.T) {
	m := managerFor(t, Config{Namespace: "a"})
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s := mustSession(t, c, "a")
		if s.Fresh() {
			s.Set("value", 1)
		}
		for _, target := range []any{nil, (*int)(nil), 1} {
			err := s.Get("value", target)
			var typed *cbor.InvalidUnmarshalError
			if !errors.As(err, &typed) {
				t.Errorf("target %T: expected codec argument error, got %v", target, err)
			}
		}
		return nil
	})
	cookies := request(t, app, "/").Cookies()
	request(t, app, "/", cookies...)
}
