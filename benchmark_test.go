package sessionmanager

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gofiber/fiber/v3"
)

func BenchmarkHTTP(b *testing.B) {
	for _, operation := range []string{"unused", "read", "write"} {
		b.Run(operation, func(b *testing.B) {
			m := New(Config{Namespace: "benchmark"})
			b.Cleanup(func() { _ = m.Close() })
			raw, err := cbor.Marshal(record{Version: 1, Values: map[string][]byte{"count": {1}}})
			if err != nil {
				b.Fatal(err)
			}
			if err := m.namespaces["benchmark"].Storage.Set("session:benchmark:existing", raw, time.Hour); err != nil {
				b.Fatal(err)
			}
			app := fiber.New()
			app.Use(m.Handler())
			app.Get("/", func(c fiber.Ctx) error {
				if operation != "unused" {
					s, err := FromContext(c, "benchmark")
					if err != nil {
						return err
					}
					var count int
					if err := s.Get("count", &count); err != nil {
						return err
					}
					if operation == "write" {
						s.Set("count", count+1)
					}
				}
				return c.SendStatus(http.StatusNoContent)
			})
			req := httptest.NewRequest("GET", "/", nil)
			req.AddCookie(&http.Cookie{Name: "session_id_benchmark", Value: "existing"})
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				response, err := app.Test(req)
				if err != nil {
					b.Fatal(err)
				}
				_ = response.Body.Close()
				if response.StatusCode != http.StatusNoContent {
					b.Fatalf("status: %d", response.StatusCode)
				}
			}
		})
	}
}
