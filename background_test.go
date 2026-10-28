package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gofiber/fiber/v3"
)

func seedSession(t *testing.T, storage fiber.Storage, ns, id string, deadline int64, values map[string]any) {
	t.Helper()
	encoded := make(map[string][]byte, len(values))
	for key, value := range values {
		encoded[key] = mustEncode(t, value)
	}
	err := storage.Set("session:"+ns+":"+id, mustEncode(t, record{Version: 1, ExpiresAt: deadline, Values: encoded}), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundReadSaveAndContext(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage, AbsoluteTimeout: time.Hour})
	now := time.Now()
	m.namespaces["a"].now = func() time.Time { return now }
	deadline := now.Add(45 * time.Minute).UnixNano()
	seedSession(t, storage, "a", "existing", deadline, map[string]any{"value": 1})
	ctx := context.WithValue(t.Context(), struct{}{}, "read")
	s, err := m.GetByID(ctx, "a", "existing")
	checkErr(t, err, nil)
	if s == nil {
		t.Fatal("missing session")
	}
	if s.Fresh() || s.request != nil || storage.gets != 1 || storage.sets != 0 || storage.seenContext != ctx {
		t.Fatal("background read changed state or lost context")
	}
	roundTrip(t, s, "value", 1, false)
	s.Set("value", 2)
	now = now.Add(5 * time.Minute)
	checkErr(t, s.Save(), nil)
	if storage.seenContext != context.Background() || storage.ttl != 30*time.Minute || s.Fresh() {
		t.Fatal("detached Save context, freshness or idle renewal")
	}
	now = now.Add(30 * time.Minute)
	checkErr(t, s.SaveWithContext(ctx), nil)
	if storage.seenContext != ctx || storage.ttl != 10*time.Minute {
		t.Fatal("absolute deadline moved or context lost")
	}
	loaded, err := m.GetByID(nil, "a", "existing")
	checkErr(t, err, nil)
	if loaded == nil || loaded.expiresAt != deadline || loaded == s {
		t.Fatal("background load must return an independent snapshot")
	}
	roundTrip(t, loaded, "value", 2, false)
	now = time.Unix(0, deadline)
	before := storage.sets
	checkErr(t, s.Save(), ErrExpired)
	_, err = m.GetByID(nil, "a", "existing")
	checkErr(t, err, ErrSessionNotFound)
	if storage.sets != before || storage.deletes != 0 {
		t.Fatal("expiry lookup mutated storage")
	}
}

func TestBackgroundLookupAndDeleteErrors(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage})
	for _, tc := range []struct {
		ns, id string
		want   error
	}{{"unknown", "id", ErrUnknownNamespace}, {"a", "", ErrInvalidSessionID}, {"a", "missing", ErrSessionNotFound}} {
		s, err := m.GetByID(nil, tc.ns, tc.id)
		checkErr(t, err, tc.want)
		if s != nil {
			t.Fatal("failed lookup returned a session")
		}
	}
	checkErr(t, m.DeleteByID(nil, "unknown", "id"), ErrUnknownNamespace)
	checkErr(t, m.DeleteByID(nil, "a", ""), ErrInvalidSessionID)
	checkErr(t, m.DeleteByID(nil, "a", "missing"), nil)
	if storage.seenContext != context.Background() {
		t.Fatal("nil context was not normalized")
	}
	ctx := context.WithValue(t.Context(), struct{}{}, "delete")
	checkErr(t, storage.Set("session:a:corrupt", []byte{0xff}, time.Minute), nil)
	_, err := m.GetByID(ctx, "a", "corrupt")
	var syntax *cbor.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("codec error lost: %v", err)
	}
	checkErr(t, m.DeleteByID(ctx, "a", "corrupt"), nil)
	if storage.seenContext != ctx {
		t.Fatal("delete context lost")
	}
	_, err = m.GetByID(ctx, "a", "corrupt")
	checkErr(t, err, ErrSessionNotFound)
	storage.getErr = errStorage
	_, err = m.GetByID(ctx, "a", "id")
	checkErr(t, err, errStorage)
	storage.deleteErr = errStorage
	checkErr(t, m.DeleteByID(ctx, "a", "id"), errStorage)
	if storage.sets != 0 {
		t.Fatal("background lookup or delete wrote a record")
	}
}

func TestBackgroundLifecycle(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage, AbsoluteTimeout: time.Hour})
	now := time.Now()
	m.namespaces["a"].now = func() time.Time { return now }
	deadline := now.Add(30 * time.Minute).UnixNano()
	seedSession(t, storage, "a", "original", deadline, map[string]any{"value": 1})
	s, err := m.GetByID(nil, "a", "original")
	checkErr(t, err, nil)
	if s == nil {
		t.Fatal("missing session")
	}
	checkErr(t, s.Regenerate(), nil)
	if s.ID() == "original" || !s.Fresh() || s.expiresAt != deadline || storage.sets != 0 {
		t.Fatal("background rotation semantics")
	}
	_, err = m.GetByID(nil, "a", "original")
	checkErr(t, err, ErrSessionNotFound)
	_, err = m.GetByID(nil, "a", s.ID())
	checkErr(t, err, ErrSessionNotFound)
	roundTrip(t, s, "value", 1, false)
	checkErr(t, s.Save(), nil)
	old := s.ID()
	s.SetIdleTimeout(time.Minute)
	now = now.Add(5 * time.Minute)
	checkErr(t, s.Reset(), nil)
	if s.ID() == old || len(s.Keys()) != 0 || s.idle != 30*time.Minute || s.expiresAt != now.Add(time.Hour).UnixNano() {
		t.Fatal("background reset semantics")
	}
	checkErr(t, s.Save(), nil)
	id := s.ID()
	checkErr(t, s.Destroy(), nil)
	_, err = m.GetByID(nil, "a", id)
	checkErr(t, err, ErrSessionNotFound)
}

// These ordered overlaps demonstrate the guarantees of the Storage contract.
// A late HTTP save can overwrite a background update or recreate a deleted key.
func TestHTTPAndBackgroundLastWriteWins(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			storage := storageFor(t)
			m := managerFor(t, Config{Namespace: "a", Storage: storage})
			seedSession(t, storage, "a", "shared", 0, map[string]any{"original": true})
			loaded, resume := make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			unblock := func() { resumeOnce.Do(func() { close(resume) }) }
			t.Cleanup(unblock)
			app := fiber.New()
			app.Use(m.Handler())
			app.Get("/", func(c fiber.Ctx) error {
				s, err := FromContext(c, "a")
				if err != nil {
					return err
				}
				close(loaded)
				<-resume
				s.Set("request", true)
				return c.SendStatus(http.StatusNoContent)
			})
			type outcome struct {
				response *http.Response
				err      error
			}
			finished := make(chan outcome, 1)
			go func() {
				req := httptest.NewRequest("GET", "/", nil)
				req.AddCookie(&http.Cookie{Name: "session_id_a", Value: "shared"})
				response, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
				finished <- outcome{response, err}
			}()
			select {
			case <-loaded:
			case result := <-finished:
				if result.response != nil {
					_ = result.response.Body.Close()
				}
				t.Fatalf("request ended before loading: %v", result.err)
			}
			if operation == "delete" {
				checkErr(t, m.DeleteByID(t.Context(), "a", "shared"), nil)
			} else {
				s, err := m.GetByID(t.Context(), "a", "shared")
				if err != nil {
					t.Fatal(err)
				}
				s.Set("background", true)
				checkErr(t, s.SaveWithContext(t.Context()), nil)
			}
			unblock()
			result := <-finished
			if result.err != nil {
				t.Fatal(result.err)
			}
			defer result.response.Body.Close()
			if result.response.StatusCode != http.StatusNoContent {
				t.Fatalf("request status: %d", result.response.StatusCode)
			}
			s, err := m.GetByID(nil, "a", "shared")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.Keys(), []string{"original", "request"}) {
				t.Fatalf("unexpected final snapshot: %v", s.Keys())
			}
		})
	}
}

func TestBackgroundSnapshotDoesNotJoinRequest(t *testing.T) {
	storage := storageFor(t)
	m := managerFor(t, Config{Namespace: "a", Storage: storage})
	seedSession(t, storage, "a", "background", 0, nil)
	app := fiber.New()
	app.Use(m.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := m.GetByID(c.Context(), "a", "background")
		if err != nil {
			return err
		}
		s.Set("unsaved", 1)
		return c.SendStatus(http.StatusNoContent)
	})
	response := request(t, app, "/")
	if storage.sets != 0 || len(response.Cookies()) != 0 {
		t.Fatal("background lookup joined automatic saving")
	}
	s, err := m.GetByID(nil, "a", "background")
	checkErr(t, err, nil)
	if s == nil || len(s.Keys()) != 0 {
		t.Fatal("unsaved background change persisted")
	}
}

func TestConcurrentDetachedReads(t *testing.T) {
	m := managerFor(t, Config{Namespace: "a"})
	seedSession(t, m.namespaces["a"].Storage, "a", "shared", 0, map[string]any{"value": "stored"})
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			s, err := m.GetByID(t.Context(), "a", "shared")
			if err != nil {
				t.Error(err)
				return
			}
			s.Set("value", fmt.Sprint(i))
			var value string
			if err := s.Get("value", &value); err != nil || value != fmt.Sprint(i) {
				t.Errorf("snapshot isolation: %q %v", value, err)
			}
		})
	}
	wg.Wait()
}
