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
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"code.vikunja.io/api/pkg/log"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactUploadTicket(t *testing.T) {
	e := echo.New()
	e.POST(mcpUploadsRoute, func(c *echo.Context) error {
		return c.String(http.StatusOK, redactUploadTicket(c, c.Request().URL.String()))
	})
	e.GET("/api/v2/tasks/:task", func(c *echo.Context) error {
		return c.String(http.StatusOK, redactUploadTicket(c, c.Request().URL.String()))
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v2/mcp/uploads/s3cr3t-ticket", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/api/v2/mcp/uploads/<redacted>", rec.Body.String())

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/tasks/1?expand=x", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/api/v2/tasks/1?expand=x", rec.Body.String())
}

func TestHTTPRequestLoggerOmitsUploadTicket(t *testing.T) {
	var buf bytes.Buffer
	e := echo.New()
	e.Use(httpRequestLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	e.POST(mcpUploadsRoute, func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v2/mcp/uploads/s3cr3t-ticket", nil))

	assert.Contains(t, buf.String(), mcpUploadsRoute)
	assert.NotContains(t, buf.String(), "s3cr3t-ticket")
}

func TestSentryEventsOmitUploadTicket(t *testing.T) {
	const ticket = "s3cr3t-ticket"
	log.InitLogger()
	var events []*sentry.Event
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://public@example.com/1",
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			events = append(events, event)
			return nil
		},
	})
	require.NoError(t, err)

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			hub := sentry.NewHub(client, sentry.NewScope())
			c.SetRequest(c.Request().WithContext(sentry.SetHubOnContext(c.Request().Context(), hub)))
			return next(c)
		}
	})
	e.Use(SentryMiddleware(SentryOptions{}))
	e.POST(mcpUploadsRoute, func(c *echo.Context) error {
		if c.QueryParam("panic") != "" {
			panic("boom for " + c.Param("ticket"))
		}
		reportToSentry(errors.New("failed for "+c.Param("ticket")), c)
		return c.NoContent(http.StatusOK)
	})

	for name, query := range map[string]string{
		"reported error": "",
		"panic":          "?panic=1",
	} {
		t.Run(name, func(t *testing.T) {
			events = nil
			e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v2/mcp/uploads/"+ticket+query, nil))

			require.Len(t, events, 1)
			for _, event := range events {
				require.NotNil(t, event.Request)
				assert.Equal(t, "http://example.com/api/v2/mcp/uploads/<redacted>", event.Request.URL)
				raw, err := json.Marshal(event)
				require.NoError(t, err)
				assert.NotContains(t, string(raw), ticket)
			}
		})
	}
}
