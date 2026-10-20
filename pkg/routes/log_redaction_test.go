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
	"net/http"
	"net/http/httptest"
	"testing"

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
