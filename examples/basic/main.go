package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v3"
	"go.z2n.io/fiber/sessionmanager"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	manager := sessionmanager.New(
		// Local HTTP: enable CookieSecure as well when serving over HTTPS.
		sessionmanager.Config{Namespace: "visits", CookieHTTPOnly: true},
		sessionmanager.Config{Namespace: "preferences", CookieHTTPOnly: true},
	)
	defer func() { err = errors.Join(err, manager.Close()) }()

	app := fiber.New()
	app.Use(manager.Handler())
	app.Get("/visits", func(c fiber.Ctx) error {
		s, err := sessionmanager.FromContext(c, "visits")
		if err != nil {
			return err
		}
		var count int
		if err := s.Get("count", &count); err != nil && !errors.Is(err, sessionmanager.ErrKeyNotFound) {
			return err
		}
		count++
		s.Set("count", count)
		return c.JSON(fiber.Map{"visits": count})
	})
	app.Get("/preferences", func(c fiber.Ctx) error {
		s, err := sessionmanager.FromContext(c, "preferences")
		if err != nil {
			return err
		}
		var theme string
		if err := s.Get("theme", &theme); errors.Is(err, sessionmanager.ErrKeyNotFound) {
			theme = "light"
			s.Set("theme", theme)
		} else if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"theme": theme})
	})
	app.Put("/preferences/:theme", func(c fiber.Ctx) error {
		theme := c.Params("theme")
		if theme != "light" && theme != "dark" {
			return fiber.NewError(fiber.StatusBadRequest, "theme must be light or dark")
		}
		s, err := sessionmanager.FromContext(c, "preferences")
		if err != nil {
			return err
		}
		s.Set("theme", theme)
		return c.JSON(fiber.Map{"theme": theme})
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Print("listening on http://127.0.0.1:3000; press Ctrl+C to stop")
	return app.Listen("127.0.0.1:3000", fiber.ListenConfig{
		GracefulContext:       ctx,
		ShutdownTimeout:       -1, // Drain requests before the deferred Manager.Close.
		DisableStartupMessage: true,
	})
}
