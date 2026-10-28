package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/gofiber/fiber/v3"
	"go.z2n.io/fiber/sessionmanager"
)

type preferences struct {
	Colors  []string `json:"colors"`
	Missing []string `json:"missing"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	manager := sessionmanager.New(sessionmanager.Config{
		Namespace:      "preferences",
		CookieSecure:   true,
		CookieHTTPOnly: true,
		Marshal:        json.Marshal,
		Unmarshal:      json.Unmarshal,
	})
	defer func() { err = errors.Join(err, manager.Close()) }()

	app := fiber.New()
	app.Use(manager.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := sessionmanager.FromContext(c, "preferences")
		if err != nil {
			return err
		}
		if s.Fresh() {
			s.Set("colors", []string{})
			s.Set("missing", []string(nil))
		}
		var result preferences
		if err := s.Get("colors", &result.Colors); err != nil {
			return err
		}
		if err := s.Get("missing", &result.Missing); err != nil {
			return err
		}
		return c.JSON(result)
	})

	// The second request loads the stored record rather than pending values.
	var cookies []*http.Cookie
	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response, err := app.Test(request)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(response.Body)
		if err := errors.Join(readErr, response.Body.Close()); err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("request failed: HTTP %d: %s", response.StatusCode, body)
		}
		cookies = response.Cookies()
		fmt.Printf("request %d: %s\n", requestNumber, body)
	}
	return nil
}
