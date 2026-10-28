package sessionmanager_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"

	"github.com/gofiber/fiber/v3"
	"go.z2n.io/fiber/sessionmanager"
)

func Example() {
	manager := sessionmanager.New(
		sessionmanager.Config{Namespace: "auth", CookieSecure: true, CookieHTTPOnly: true},
		sessionmanager.Config{Namespace: "preferences", CookieSecure: true, CookieHTTPOnly: true},
	)
	defer manager.Close() // after HTTP requests and background operations finish
	app := fiber.New()
	app.Use(manager.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		auth, err := sessionmanager.FromContext(c, "auth")
		if err != nil {
			return err
		}
		preferences, err := sessionmanager.FromContext(c, "preferences")
		if err != nil {
			return err
		}
		auth.Set("authenticated", true)
		preferences.Set("theme", "dark")
		var authenticated bool
		if err := auth.Get("authenticated", &authenticated); err != nil {
			return err
		}
		return c.SendString(fmt.Sprint(authenticated))
	})
	response, err := app.Test(httptest.NewRequest("GET", "https://example.test/", nil))
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(body), len(response.Cookies()))
	// Output: true 2
}

func ExampleManager_GetByID() {
	manager := sessionmanager.New(sessionmanager.Config{Namespace: "preferences"})
	defer manager.Close()
	app := fiber.New()
	app.Use(manager.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := sessionmanager.FromContext(c, "preferences")
		if err != nil {
			return err
		}
		s.Set("theme", "light")
		return nil // the Manager saves this HTTP session
	})
	response, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	id := response.Cookies()[0].Value // obtain IDs through the application's own workflow

	ctx := context.Background()
	s, err := manager.GetByID(ctx, "preferences", id)
	if err != nil {
		panic(err)
	}
	s.Set("theme", "dark")
	if err := s.SaveWithContext(ctx); err != nil {
		panic(err)
	}
	var theme string
	if err := s.Get("theme", &theme); err != nil {
		panic(err)
	}
	fmt.Println(theme)
	// Output: dark
}

func ExampleConfig() {
	manager := sessionmanager.New(sessionmanager.Config{
		Namespace: "preferences",
		Marshal:   json.Marshal,
		Unmarshal: json.Unmarshal,
	})
	defer manager.Close()
	app := fiber.New()
	app.Use(manager.Handler())
	app.Get("/", func(c fiber.Ctx) error {
		s, err := sessionmanager.FromContext(c, "preferences")
		if err != nil {
			return err
		}
		s.Set("colors", []string{})
		var colors []string
		if err := s.Get("colors", &colors); err != nil {
			return err
		}
		return c.JSON(colors)
	})
	response, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(body))
	// Output: []
}
