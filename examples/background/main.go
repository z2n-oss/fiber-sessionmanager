package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gofiber/fiber/v3"
	"go.z2n.io/fiber/sessionmanager"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	manager := sessionmanager.New(sessionmanager.Config{
		Namespace:       "preferences",
		CookieSecure:    true,
		CookieHTTPOnly:  true,
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 24 * time.Hour,
	})
	defer func() { err = errors.Join(err, manager.Close()) }()

	// Create the initial record through HTTP; GetByID only loads existing sessions.
	app := fiber.New()
	app.Use(manager.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := sessionmanager.FromContext(c, "preferences")
		if err != nil {
			return err
		}
		s.Set("theme", "light")
		return c.SendStatus(fiber.StatusNoContent)
	})
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "https://example.test/", nil))
	if err != nil {
		return err
	}
	if err := response.Body.Close(); err != nil {
		return err
	}
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("session creation failed: HTTP %d", response.StatusCode)
	}
	var id string
	for _, cookie := range response.Cookies() {
		if cookie.Name == "session_id_preferences" {
			id = cookie.Value
		}
	}
	if id == "" {
		return errors.New("session cookie is missing")
	}

	// HTTP handling has finished. Only the ID crosses into this separate operation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := manager.GetByID(ctx, "preferences", id)
	if err != nil {
		return err
	}
	var theme string
	if err := s.Get("theme", &theme); err != nil {
		return err
	}
	fmt.Println("before:", theme)
	s.Set("theme", "dark")
	if err := s.SaveWithContext(ctx); err != nil {
		return err
	}

	// Reload to observe the persisted value, not the first object's pending map.
	loaded, err := manager.GetByID(ctx, "preferences", id)
	if err != nil {
		return err
	}
	if err := loaded.Get("theme", &theme); err != nil {
		return err
	}
	fmt.Println("after:", theme)
	if err := manager.DeleteByID(ctx, "preferences", id); err != nil {
		return err
	}
	_, err = manager.GetByID(ctx, "preferences", id)
	if !errors.Is(err, sessionmanager.ErrSessionNotFound) {
		return fmt.Errorf("expected ErrSessionNotFound after deletion, got %v", err)
	}
	fmt.Println("deleted: true")
	return nil
}
