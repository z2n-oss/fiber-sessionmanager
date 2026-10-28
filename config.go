package sessionmanager

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/log"
	"github.com/gofiber/utils/v2"
)

// Config registers one independent namespace. Storage and callbacks must support
// concurrent calls. Do not mutate Extractor trees or callback configuration after New.
type Config struct {
	// Namespace is required and accepts ASCII letters, digits, underscores and hyphens.
	Namespace string
	// Storage defaults to a manager-owned memory store. Supplied stores remain caller-owned.
	Storage fiber.Storage
	// Extractor defaults to the cookie session_id_<namespace>.
	Extractor extractors.Extractor
	// KeyGenerator defaults to utils.SecureToken. Custom IDs must be unique,
	// unpredictable, nonempty and suitable for the configured transports.
	KeyGenerator func() string
	// Marshal and Unmarshal encode values and the record envelope. Supply both or
	// neither; defaults are cbor.Marshal and cbor.Unmarshal. See the package Codec contract.
	Marshal   func(any) ([]byte, error)
	Unmarshal func([]byte, any) error
	// ErrorHandler handles automatic save failures only. Defaults to DefaultErrorHandler.
	ErrorHandler func(fiber.Ctx, error)
	// CookieDomain defaults to an empty, host-only domain.
	CookieDomain string
	// CookiePath defaults to / and must start with /.
	CookiePath string
	// CookieSameSite accepts Disabled, Lax, Strict or None (case-insensitive).
	// Defaults to Lax. None forces CookieSecure.
	CookieSameSite string
	// CookieSecure restricts cookies to HTTPS. Defaults to false; enable in production.
	CookieSecure bool
	// CookieHTTPOnly prevents JavaScript access. Defaults to false; enable in production.
	CookieHTTPOnly bool
	// CookieSessionOnly omits client expiry attributes; server-side expiry still applies.
	CookieSessionOnly bool
	// IdleTimeout defaults to 30 minutes and is refreshed on every successful Save.
	IdleTimeout time.Duration
	// AbsoluteTimeout limits total lifetime. Zero disables it; positive values
	// must be at least IdleTimeout. Only creation and Reset start a new deadline.
	AbsoluteTimeout time.Duration
}

type namespaceConfig struct {
	Config
	sinks []identifierSink
	now   func() time.Time
}

// DefaultErrorHandler logs an automatic save failure and responds with HTTP 500.
// A downstream business error is still returned to Fiber and may replace this response.
func DefaultErrorHandler(c fiber.Ctx, err error) {
	log.Errorf("session error: %v", err)
	// A handler may already have prepared an encoded response or redirect.
	// Preserve unrelated headers, including cookies from other namespaces.
	c.Response().Header.Del(fiber.HeaderContentEncoding)
	c.Response().Header.Del(fiber.HeaderETag)
	c.Response().Header.Del(fiber.HeaderLocation)
	c.Type("txt", "utf-8")
	if sendErr := c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error"); sendErr != nil {
		log.Errorf("failed to send session error response: %v", sendErr)
	}
}

var namespacePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func normalize(c Config) namespaceConfig {
	invalid := func(reason string) {
		panic(fmt.Sprintf("sessionmanager namespace %q: %s", c.Namespace, reason))
	}
	if !namespacePattern.MatchString(c.Namespace) {
		invalid("invalid namespace")
	}
	if c.Storage != nil {
		v := reflect.ValueOf(c.Storage)
		switch v.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if v.IsNil() {
				invalid("typed-nil storage")
			}
		}
	}
	if c.IdleTimeout < 0 || c.AbsoluteTimeout < 0 {
		invalid("negative timeout")
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 30 * time.Minute
	}
	if c.AbsoluteTimeout != 0 && c.AbsoluteTimeout < c.IdleTimeout {
		invalid("absolute timeout is shorter than idle timeout")
	}
	if _, err := absoluteDeadline(time.Now(), c.AbsoluteTimeout); err != nil {
		invalid("absolute timeout exceeds the supported Unix nanosecond range")
	}
	if (c.Marshal == nil) != (c.Unmarshal == nil) {
		invalid("both codec functions are required")
	}
	if c.Marshal == nil {
		c.Marshal, c.Unmarshal = cbor.Marshal, cbor.Unmarshal
	}
	if c.Extractor.Extract == nil && len(c.Extractor.Chain) == 0 {
		if c.Extractor.Key != "" || c.Extractor.AuthScheme != "" || c.Extractor.Source != 0 {
			invalid("extractor has metadata but no extraction function")
		}
		c.Extractor = extractors.FromCookie("session_id_" + c.Namespace)
	}
	if c.KeyGenerator == nil {
		c.KeyGenerator = utils.SecureToken
	}
	if c.ErrorHandler == nil {
		c.ErrorHandler = DefaultErrorHandler
	}
	if c.CookiePath == "" {
		c.CookiePath = "/"
	}
	if !strings.HasPrefix(c.CookiePath, "/") {
		invalid("cookie path must start with /")
	}
	c.CookieSameSite = strings.ToLower(c.CookieSameSite)
	switch c.CookieSameSite {
	case "":
		c.CookieSameSite = "lax"
	case "none":
		c.CookieSecure = true
	case "disabled", "lax", "strict":
	default:
		invalid("invalid SameSite")
	}
	if err := (&http.Cookie{Name: "session", Domain: c.CookieDomain, Path: c.CookiePath}).Valid(); err != nil {
		invalid("invalid cookie domain or path")
	}

	n := namespaceConfig{Config: c, now: time.Now}
	seen := make(map[identifierSink]bool)
	c.Extractor.Walk(func(e extractors.Extractor) bool {
		if len(e.Chain) != 0 || e.Extract == nil || (e.Source != extractors.SourceCookie && e.Source != extractors.SourceHeader) {
			return true
		}
		if !validFieldName(e.Key) {
			invalid("invalid cookie or header name")
		}
		if e.Source == extractors.SourceCookie {
			if strings.HasPrefix(e.Key, "__Secure-") && !c.CookieSecure {
				invalid("__Secure- cookies require Secure")
			}
			if strings.HasPrefix(e.Key, "__Host-") && (!c.CookieSecure || c.CookieDomain != "" || c.CookiePath != "/") {
				invalid("__Host- cookies require Secure, Path=/ and an empty Domain")
			}
		}
		sink := identifierSink{source: e.Source, key: e.Key}
		identity := sink.identity()
		if !seen[identity] {
			n.sinks = append(n.sinks, sink)
			seen[identity] = true
		}
		return true
	})
	return n
}

func (n *namespaceConfig) storageKey(id string) string {
	return "session:" + n.Namespace + ":" + id
}

func (n *namespaceConfig) wrap(op, key string, err error) error {
	if key != "" {
		return fmt.Errorf("session namespace %q %s key %q: %w", n.Namespace, op, key, err)
	}
	return fmt.Errorf("session namespace %q %s: %w", n.Namespace, op, err)
}

func (n *namespaceConfig) newID(previous string) (string, error) {
	id := strings.Clone(n.KeyGenerator())
	if id == previous || validateIdentifier(id, n.sinks) != nil {
		return "", ErrInvalidSessionID
	}
	return id, nil
}
