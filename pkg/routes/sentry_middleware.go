// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package routes

import (
	"context"
	"net/http"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v5"
)

// sentryHubKey is the context key for storing the Sentry hub
type sentryHubKey struct{}

// SentryOptions holds options for the sentry middleware
type SentryOptions struct {
	// Repanic configures whether to repanic after recovery
	Repanic bool
}

// SentryMiddleware returns a middleware that captures panics and reports them to Sentry.
// It also attaches a Sentry hub to the request context.
func SentryMiddleware(options SentryOptions) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			hub := sentry.GetHubFromContext(c.Request().Context())
			if hub == nil {
				hub = sentry.CurrentHub().Clone()
			}

			scope := hub.Scope()
			scope.SetRequest(c.Request())
			scope.SetRequestBody(nil) // We don't want to log request bodies
			redactLinkTicketFromEvents(c, scope)

			// Store hub in context
			ctx := context.WithValue(c.Request().Context(), sentryHubKey{}, hub)
			c.SetRequest(c.Request().WithContext(ctx))

			defer func() {
				if err := recover(); err != nil {
					eventID := hub.RecoverWithContext(
						context.WithValue(c.Request().Context(), sentry.RequestContextKey, c.Request()),
						err,
					)
					if eventID != nil && options.Repanic {
						panic(err)
					}
				}
			}()

			return next(c)
		}
	}
}

// scope.SetRequest makes Sentry build the event's request URL from the raw path, which holds the ticket
// of an upload or download link, so events from those routes are rewritten before they are sent.
func redactLinkTicketFromEvents(c *echo.Context, scope *sentry.Scope) {
	ticket := c.Param("ticket")
	if !isLinkTicketRoute(c) || ticket == "" {
		return
	}
	redact := func(s string) string { return strings.ReplaceAll(s, ticket, "<redacted>") }
	scope.AddEventProcessor(func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
		event.Message = redact(event.Message)
		for i := range event.Exception {
			event.Exception[i].Value = redact(event.Exception[i].Value)
		}
		if r := event.Request; r != nil {
			r.URL = redact(r.URL)
			r.QueryString = redact(r.QueryString)
			r.Data = redact(r.Data)
			r.Cookies = redact(r.Cookies)
			for k, v := range r.Headers {
				r.Headers[k] = redact(v)
			}
			for k, v := range r.Env {
				r.Env[k] = redact(v)
			}
		}
		return event
	})
}

// GetSentryHubFromContext retrieves the Sentry hub from the echo context
func GetSentryHubFromContext(c *echo.Context) *sentry.Hub {
	if hub, ok := c.Request().Context().Value(sentryHubKey{}).(*sentry.Hub); ok {
		return hub
	}
	return nil
}

// GetSentryHubFromRequest retrieves the Sentry hub from the HTTP request context
func GetSentryHubFromRequest(r *http.Request) *sentry.Hub {
	if hub, ok := r.Context().Value(sentryHubKey{}).(*sentry.Hub); ok {
		return hub
	}
	return nil
}
