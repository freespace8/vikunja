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

package webtests

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"
	"code.vikunja.io/api/pkg/modules/keyvalue"
	"code.vikunja.io/api/pkg/routes"
	"code.vikunja.io/api/pkg/user"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	uploadLinkTool = "task_attachments_upload_link"
	// Project 10 shares write access with user 1; project 3 only read access.
	writableSharedTask = 19
	readOnlyTask       = 32
)

type uploadLink struct {
	UploadURL        string    `json:"upload_url"`
	ExpiresAt        time.Time `json:"expires_at"`
	MaxFileSizeBytes int64     `json:"max_file_size_bytes"`
	CurlExample      string    `json:"curl_example"`
}

func createAttachmentToken(t *testing.T, permissions models.APIPermissions) *models.APIToken {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	owner, err := user.GetUserByID(s, 1)
	require.NoError(t, err)
	token := &models.APIToken{
		Title:          "attachment upload",
		APIPermissions: permissions,
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	require.NoError(t, token.Create(s, owner))
	require.NoError(t, s.Commit())
	return token
}

func uploadPermissions() models.APIPermissions {
	return models.APIPermissions{
		"mcp":               {"access"},
		"tasks_attachments": {"create"},
	}
}

func mintViaMCP(t *testing.T, c *mcpClient, task int64) uploadLink {
	t.Helper()
	var link uploadLink
	toolResultJSON(t, c.callTool(uploadLinkTool, map[string]any{"task": task}), &link)
	return link
}

func multipartFile(t *testing.T, name string, size int) (body *bytes.Buffer, contentType string) {
	t.Helper()
	body = &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("files", name)
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("a"), size))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return body, w.FormDataContentType()
}

// redeem uploads without any Authorization header: the ticket is the credential.
func redeem(t *testing.T, e *echo.Echo, uploadURL string, size int) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(uploadURL)
	require.NoError(t, err)
	body, contentType := multipartFile(t, "image.png", size)
	req := httptest.NewRequest(http.MethodPost, u.Path, body)
	req.Header.Set(echo.HeaderContentType, contentType)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

type uploadResult struct {
	Errors  []map[string]any `json:"errors"`
	Success []struct {
		ID     int64 `json:"id"`
		TaskID int64 `json:"task_id"`
	} `json:"success"`
}

func attachmentCount(t *testing.T, taskID int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("task_id = ?", taskID).Count(&models.TaskAttachment{})
	require.NoError(t, err)
	return n
}

func TestMCP_AttachmentUpload_ToolFollowsScopes(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	with := createAttachmentToken(t, uploadPermissions())
	names := newMCPClientOn(t, e, with.Token).toolNames()
	assert.Equal(t, map[string]bool{uploadLinkTool: true}, names)

	for name, permissions := range map[string]models.APIPermissions{
		"read and delete only": {
			"mcp":               {"access"},
			"tasks_attachments": {"read_all", "delete"},
		},
		"tasks create": {
			"mcp":   {"access"},
			"tasks": {"create", "update"},
		},
	} {
		token := createAttachmentToken(t, permissions)
		assert.NotContains(t, newMCPClientOn(t, e, token.Token).toolNames(), uploadLinkTool, name)
	}
	assert.NotContains(t, newMCPClientOn(t, e, mcpFullToken).toolNames(), uploadLinkTool)

	desc := newMCPClientOn(t, e, with.Token).toolDescription(uploadLinkTool)
	assert.Contains(t, desc, "single-use")
	assert.Contains(t, desc, "upload_url")
	assert.Contains(t, desc, "curl")
}

func TestMCP_AttachmentUpload_NotAnAPITokenPermissionOfItsOwn(t *testing.T) {
	_, err := setupTestEnv()
	require.NoError(t, err)
	routesByGroup := models.GetAPITokenRoutes()
	assert.NotContains(t, routesByGroup["tasks"], "attachments_upload_link")
	// The shared key keeps its v1 entry in this merged view; the v2 upload route must still be authorised by it.
	create := &models.APIToken{APIPermissions: models.APIPermissions{"tasks_attachments": {"create"}}}
	assert.True(t, create.CanUseRoute("/api/v2/tasks/:task/attachments", http.MethodPost))
	assert.True(t, create.CanUseRoute(models.TaskAttachmentUploadLinkRoute, http.MethodPost))
	for group := range routesByGroup {
		assert.False(t, strings.HasPrefix(group, "mcp_"), group)
	}
	assert.NotContains(t, routesByGroup["other"], "mcp_uploads")
}

func TestMCP_AttachmentUpload_RoundTrip(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := createAttachmentToken(t, uploadPermissions())
	c := newMCPClientOn(t, e, token.Token)

	link := mintViaMCP(t, c, 1)
	assert.True(t, strings.HasPrefix(link.UploadURL, "https://localhost/api/v2/mcp/uploads/"), link.UploadURL)
	assert.Contains(t, link.CurlExample, "curl -X POST '"+link.UploadURL+"'")
	assert.Contains(t, link.CurlExample, "-F 'files=@")
	assert.WithinDuration(t, time.Now().Add(10*time.Minute), link.ExpiresAt, time.Minute)
	assert.Equal(t, config.GetMaxFileSizeInMBytes()*1024*1024, uint64(link.MaxFileSizeBytes))

	before := attachmentCount(t, 1)
	// Larger than the 4 MiB MCP request cap, which must not apply to the upload route.
	rec := redeem(t, e, link.UploadURL, 5<<20)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var result uploadResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Empty(t, result.Errors)
	require.Len(t, result.Success, 1)
	assert.Equal(t, int64(1), result.Success[0].TaskID)
	assert.Equal(t, before+1, attachmentCount(t, 1))

	// Single use.
	rec = redeem(t, e, link.UploadURL, 10)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Equal(t, before+1, attachmentCount(t, 1))
}

func TestMCP_AttachmentUpload_SessionCannotMint(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/1/attachments/upload-link", "", humaTokenFor(t, &testuser1), "")
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "API token")
}

func TestMCP_AttachmentUpload_MintRequiresAUserWithTheScope(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	mint := func(token string) *httptest.ResponseRecorder {
		return humaRequest(t, e, http.MethodPost, "/api/v2/tasks/1/attachments/upload-link", "", token, "")
	}
	assert.Equal(t, http.StatusUnauthorized, mint("").Code)
	// An API token without tasks_attachments.create may not mint, even with mcp access.
	assert.Equal(t, http.StatusUnauthorized, mint(mcpFullToken).Code)
	assert.Equal(t, http.StatusCreated, mint(createAttachmentToken(t, uploadPermissions()).Token).Code)

	shareToken, err := auth.NewLinkShareJWTAuthtoken(&models.LinkSharing{
		ID:          1,
		Hash:        "test",
		ProjectID:   1,
		Permission:  models.PermissionWrite,
		SharingType: models.SharingTypeWithoutPassword,
		SharedByID:  1,
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, mint(shareToken).Code)
}

func TestMCP_AttachmentUpload_InvalidTicket(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	for _, ticket := range []string{"nope", strings.Repeat("a", 43)} {
		rec := redeem(t, e, "https://localhost/api/v2/mcp/uploads/"+ticket, 10)
		assert.Equal(t, http.StatusNotFound, rec.Code, ticket)
	}
}

// Expiry is the store evicting the key; the TTL itself is covered by the apiv2 unit test.
func TestMCP_AttachmentUpload_EvictedTicket(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	c := newMCPClientOn(t, e, createAttachmentToken(t, uploadPermissions()).Token)
	link := mintViaMCP(t, c, 1)
	require.NoError(t, keyvalue.DelPrefix("task_attachment_upload_ticket_"))
	assert.Equal(t, http.StatusNotFound, redeem(t, e, link.UploadURL, 10).Code)
}

func TestMCP_AttachmentUpload_CannotMintWithoutWriteAccess(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	c := newMCPClientOn(t, e, createAttachmentToken(t, uploadPermissions()).Token)
	res := c.callTool(uploadLinkTool, map[string]any{"task": readOnlyTask})
	assert.Equal(t, true, res["isError"])
	assert.Contains(t, toolResultText(t, res), "403")
	res = c.callTool(uploadLinkTool, map[string]any{"task": 99999})
	assert.Equal(t, true, res["isError"])
}

func TestMCP_AttachmentUpload_RedeemChecksPermissionAgain(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	c := newMCPClientOn(t, e, createAttachmentToken(t, uploadPermissions()).Token)

	link := mintViaMCP(t, c, writableSharedTask)

	s := db.NewSession()
	_, err = s.Where("user_id = ? AND project_id = ?", 1, 10).Delete(&models.ProjectUser{})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	before := attachmentCount(t, writableSharedTask)
	rec := redeem(t, e, link.UploadURL, 10)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, before, attachmentCount(t, writableSharedTask))
	// The failed attempt used the ticket up.
	assert.Equal(t, http.StatusNotFound, redeem(t, e, link.UploadURL, 10).Code)
}

func TestMCP_AttachmentUpload_RedeemRejectsDeadTokens(t *testing.T) {
	for name, kill := range map[string]func(t *testing.T, token *models.APIToken){
		"deleted": func(t *testing.T, token *models.APIToken) {
			s := db.NewSession()
			defer s.Close()
			owner, err := user.GetUserByID(s, 1)
			require.NoError(t, err)
			require.NoError(t, token.Delete(s, owner))
			require.NoError(t, s.Commit())
		},
		"expired": func(t *testing.T, token *models.APIToken) {
			s := db.NewSession()
			defer s.Close()
			_, err := s.ID(token.ID).Cols("expires_at").Update(&models.APIToken{ExpiresAt: time.Now().Add(-time.Minute)})
			require.NoError(t, err)
			require.NoError(t, s.Commit())
		},
	} {
		t.Run(name, func(t *testing.T) {
			e, err := setupTestEnv()
			require.NoError(t, err)
			token := createAttachmentToken(t, uploadPermissions())
			link := mintViaMCP(t, newMCPClientOn(t, e, token.Token), 1)

			kill(t, token)

			before := attachmentCount(t, 1)
			rec := redeem(t, e, link.UploadURL, 10)
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			assert.Equal(t, before, attachmentCount(t, 1))
		})
	}
}

func TestMCP_AttachmentUpload_RedeemRejectsUnavailableOwner(t *testing.T) {
	for name, status := range map[string]user.Status{
		"disabled": user.StatusDisabled,
		"locked":   user.StatusAccountLocked,
	} {
		t.Run(name, func(t *testing.T) {
			e, err := setupTestEnv()
			require.NoError(t, err)
			link := mintViaMCP(t, newMCPClientOn(t, e, createAttachmentToken(t, uploadPermissions()).Token), 1)

			s := db.NewSession()
			_, err = s.ID(1).Cols("status").Update(&user.User{Status: status})
			require.NoError(t, err)
			require.NoError(t, s.Commit())
			s.Close()

			before := attachmentCount(t, 1)
			rec := redeem(t, e, link.UploadURL, 10)
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			assert.Equal(t, before, attachmentCount(t, 1))
		})
	}
}

func TestMCP_AttachmentUpload_SizeLimitIsPerFile(t *testing.T) {
	_, err := setupTestEnv()
	require.NoError(t, err)
	previous := config.FilesMaxSize.GetString()
	config.FilesMaxSize.Set("1MB")
	t.Cleanup(func() {
		config.FilesMaxSize.Set(previous)
		config.InitConfig()
	})
	config.InitConfig()
	// withUploadLimits reads the limit at registration time.
	e := routes.NewEcho()
	routes.RegisterRoutes(e)

	c := newMCPClientOn(t, e, createAttachmentToken(t, uploadPermissions()).Token)
	link := mintViaMCP(t, c, 1)
	assert.Equal(t, int64(1<<20), link.MaxFileSizeBytes)

	before := attachmentCount(t, 1)
	rec := redeem(t, e, link.UploadURL, 1<<20+1024)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var result uploadResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Empty(t, result.Success)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0]["message"], "exceeds")
	assert.Equal(t, before, attachmentCount(t, 1))
}
