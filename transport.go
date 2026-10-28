package sessionmanager

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
)

type requestBinding struct {
	ctx    fiber.Ctx
	owner  *container
	source extractors.Result
}

type identifierSink struct {
	source extractors.Source
	key    string
}

func (s identifierSink) identity() identifierSink {
	if s.source == extractors.SourceHeader {
		s.key = strings.ToLower(s.key)
	}
	return s
}

func validFieldName(name string) bool {
	// Cookie names and HTTP field names both use the HTTP token grammar.
	return (&http.Cookie{Name: name}).Valid() == nil
}

func validateIdentifier(id string, sinks []identifierSink) error {
	if id == "" {
		return ErrInvalidSessionID
	}
	for _, sink := range sinks {
		if sink.source == extractors.SourceCookie {
			if (&http.Cookie{Name: sink.key, Value: id}).Valid() != nil {
				return ErrInvalidSessionID
			}
		} else {
			for i := range len(id) {
				if (id[i] < ' ' && id[i] != '\t') || id[i] == 0x7f {
					return ErrInvalidSessionID
				}
			}
		}
	}
	return nil
}

func (s *Session) defaultContext() context.Context {
	if s.request != nil {
		return s.request.ctx.Context()
	}
	return context.Background()
}

func (s *Session) sinks() []identifierSink {
	if s.request == nil {
		return nil
	}
	source := s.request.source
	if source.Value != "" {
		if source.Resolved && source.Key != "" && (source.Source == extractors.SourceCookie || source.Source == extractors.SourceHeader) {
			return []identifierSink{{source: source.Source, key: source.Key}}
		}
		if !s.fresh {
			return nil
		}
	}
	return s.config.sinks
}

func (s *Session) writeIdentifier(ttl time.Duration, remove bool) {
	if s.request == nil {
		return
	}
	c := s.request.ctx
	now := s.config.now()
	for _, sink := range s.sinks() {
		if sink.source == extractors.SourceHeader {
			if remove {
				c.Request().Header.Del(sink.key)
				c.Response().Header.Del(sink.key)
			} else {
				c.Set(sink.key, s.id)
			}
			continue
		}
		cookie := &fiber.Cookie{
			Name: sink.key, Value: s.id, Domain: s.config.CookieDomain, Path: s.config.CookiePath,
			Secure: s.config.CookieSecure, HTTPOnly: s.config.CookieHTTPOnly,
			SameSite: s.config.CookieSameSite, SessionOnly: s.config.CookieSessionOnly,
		}
		if !cookie.SessionOnly {
			deadline := now.Add(ttl)
			if s.expiresAt != 0 && deadline.After(time.Unix(0, s.expiresAt)) {
				deadline = time.Unix(0, s.expiresAt)
			}
			remaining := deadline.Sub(now)
			cookie.Expires = deadline
			cookie.MaxAge = int(min(int64(remaining/time.Second), int64(math.MaxInt32)))
			if remaining <= 0 {
				cookie.MaxAge = -1
			}
		}
		if remove {
			c.Request().Header.DelCookie(sink.key)
			cookie.Value = ""
			cookie.SessionOnly = false
			cookie.MaxAge = -1
			cookie.Expires = now.Add(-time.Minute)
		}
		c.Cookie(cookie)
	}
}
