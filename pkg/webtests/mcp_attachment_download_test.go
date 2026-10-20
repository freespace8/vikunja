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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"
	"code.vikunja.io/api/pkg/modules/keyvalue"
	"code.vikunja.io/api/pkg/user"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const downloadLinkTool = "task_attachments_download_link"

type downloadLink struct {
	DownloadURL string    `json:"download_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	File        struct {
		Name      string `json:"name"`
		Mime      string `json:"mime"`
		SizeBytes int64  `json:"size_bytes"`
	} `json:"file"`
	CurlExample string `json:"curl_example"`
}

func downloadPermissions() models.APIPermissions {
	return models.APIPermissions{
		"mcp":               {"access"},
		"tasks_attachments": {"read_one"},
	}
}

func mintDownloadViaMCP(t *testing.T, c *mcpClient, task, attachment int64) downloadLink {
	t.Helper()
	var link downloadLink
	toolResultJSON(t, c.callTool(downloadLinkTool, map[string]any{
		"task":       task,
		"attachment": attachment,
	}), &link)
	return link
}

// uploadAttachmentAs stores real bytes for an attachment (fixture files have none) as user 1.
func uploadAttachmentAs(t *testing.T, e *echo.Echo, taskID int64, name string, content []byte) int64 {
	t.Helper()
	body, contentType := multipartFilesBody(t, map[string][]byte{name: content})
	rec := uploadAttachmentRequest(t, e, strconv.FormatInt(taskID, 10), body, contentType, humaTokenFor(t, &testuser1))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var result uploadResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result.Success, 1)
	return result.Success[0].ID
}

// redeemDownload fetches without credentials unless authorization is given: the ticket is the credential.
func redeemDownload(t *testing.T, e *echo.Echo, downloadURL, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(downloadURL)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, u.Path, nil)
	if authorization != "" {
		req.Header.Set(echo.HeaderAuthorization, authorization)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// mintFreshDownload uploads a file to task 1 and mints a link for it with a new token.
func mintFreshDownload(t *testing.T, e *echo.Echo, permissions models.APIPermissions) (*models.APIToken, downloadLink, int64) {
	t.Helper()
	attachment := uploadAttachmentAs(t, e, 1, "report.txt", []byte("contents"))
	token := createAttachmentToken(t, permissions)
	return token, mintDownloadViaMCP(t, newMCPClientOn(t, e, token.Token), 1, attachment), attachment
}

func TestMCP_AttachmentDownload_ToolFollowsScopes(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	with := createAttachmentToken(t, downloadPermissions())
	assert.Equal(t, map[string]bool{downloadLinkTool: true}, newMCPClientOn(t, e, with.Token).toolNames())

	// Each kind of link follows its own permission.
	upload := createAttachmentToken(t, uploadPermissions())
	assert.NotContains(t, newMCPClientOn(t, e, upload.Token).toolNames(), downloadLinkTool)
	both := createAttachmentToken(t, models.APIPermissions{
		"mcp": {"access"},
		"tasks_attachments": {
			"create",
			"read_one",
		},
	})
	assert.Equal(t, map[string]bool{
		uploadLinkTool:   true,
		downloadLinkTool: true,
	}, newMCPClientOn(t, e, both.Token).toolNames())

	for name, permissions := range map[string]models.APIPermissions{
		"list and delete only": {
			"mcp": {"access"},
			"tasks_attachments": {
				"read_all",
				"delete",
			},
		},
		"tasks read": {
			"mcp": {"access"},
			"tasks": {
				"read_one",
				"read_all",
			},
		},
	} {
		token := createAttachmentToken(t, permissions)
		assert.NotContains(t, newMCPClientOn(t, e, token.Token).toolNames(), downloadLinkTool, name)
	}
	assert.NotContains(t, newMCPClientOn(t, e, mcpFullToken).toolNames(), downloadLinkTool)

	desc := newMCPClientOn(t, e, with.Token).toolDescription(downloadLinkTool)
	assert.Contains(t, desc, "single-use")
	assert.Contains(t, desc, "10 minutes")
	assert.Contains(t, desc, "download_url")
	assert.Contains(t, desc, "curl")
	assert.Contains(t, desc, "local file")
}

func TestMCP_AttachmentDownload_NotAnAPITokenPermissionOfItsOwn(t *testing.T) {
	_, err := setupTestEnv()
	require.NoError(t, err)
	routesByGroup := models.GetAPITokenRoutes()
	assert.NotContains(t, routesByGroup["tasks"], "attachments_download_link")
	assert.NotContains(t, routesByGroup["tasks_attachments"], "download_link")
	assert.NotContains(t, routesByGroup["other"], "mcp_downloads")
	// The permission that governs minting is the one the normal download route uses.
	assert.Contains(t, routesByGroup["tasks_attachments"], "read_one")

	readOne := &models.APIToken{APIPermissions: models.APIPermissions{"tasks_attachments": {"read_one"}}}
	assert.True(t, readOne.CanUseRoute("/api/v2/tasks/:task/attachments/:attachment", http.MethodGet))
	assert.True(t, readOne.CanUseRoute(models.TaskAttachmentDownloadLinkRoute, http.MethodPost))
	assert.False(t, readOne.CanUseRoute(models.TaskAttachmentUploadLinkRoute, http.MethodPost))
}

func TestMCP_AttachmentDownload_RoundTrip(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	content := []byte("downloadable content")
	attachment := uploadAttachmentAs(t, e, 1, "download.txt", content)
	token := createAttachmentToken(t, downloadPermissions())
	c := newMCPClientOn(t, e, token.Token)

	link := mintDownloadViaMCP(t, c, 1, attachment)
	assert.True(t, strings.HasPrefix(link.DownloadURL, "https://localhost/api/v2/mcp/downloads/"), link.DownloadURL)
	assert.WithinDuration(t, time.Now().Add(10*time.Minute), link.ExpiresAt, time.Minute)
	assert.Equal(t, "download.txt", link.File.Name)
	assert.NotEmpty(t, link.File.Mime)
	assert.Equal(t, int64(len(content)), link.File.SizeBytes)
	assert.Equal(t, "curl -fsS -o 'download.txt' '"+link.DownloadURL+"'", link.CurlExample)

	rec := redeemDownload(t, e, link.DownloadURL, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, content, rec.Body.Bytes(), "the streamed file bytes must match the original")

	// Same headers as the authenticated download.
	normal := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1/attachments/"+strconv.FormatInt(attachment, 10), "", humaTokenFor(t, &testuser1), "")
	require.Equal(t, http.StatusOK, normal.Code)
	for _, header := range []string{
		"Content-Type",
		"Content-Disposition",
		"Content-Length",
		"Cache-Control",
		"Last-Modified",
		"X-Content-Type-Options",
	} {
		assert.NotEmpty(t, rec.Header().Get(header), header)
		assert.Equal(t, normal.Header().Get(header), rec.Header().Get(header), header)
	}
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "download.txt")
	assert.Equal(t, strconv.Itoa(len(content)), rec.Header().Get("Content-Length"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))

	// Single use.
	assert.Equal(t, http.StatusNotFound, redeemDownload(t, e, link.DownloadURL, "").Code)
}

func TestMCP_AttachmentDownload_AuthorizationHeaderIsIgnored(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	_, link, _ := mintFreshDownload(t, e, downloadPermissions())
	rec := redeemDownload(t, e, link.DownloadURL, "Bearer not-a-valid-token")
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestMCP_AttachmentDownload_CurlExampleQuotesTheFilename(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	attachment := uploadAttachmentAs(t, e, 1, "it's $(touch x) #1.txt", []byte("x"))
	c := newMCPClientOn(t, e, createAttachmentToken(t, downloadPermissions()).Token)

	link := mintDownloadViaMCP(t, c, 1, attachment)
	assert.Equal(t, "it's $(touch x) #1.txt", link.File.Name)
	assert.Equal(t, `curl -fsS -o 'it'\''s $(touch x) _1.txt' '`+link.DownloadURL+"'", link.CurlExample)
}

func TestMCP_AttachmentDownload_SessionCannotMint(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/1/attachments/1/download-link", "", humaTokenFor(t, &testuser1), "")
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "API token")
}

func TestMCP_AttachmentDownload_MintRequiresAUserWithTheScope(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	mint := func(token string) *httptest.ResponseRecorder {
		return humaRequest(t, e, http.MethodPost, "/api/v2/tasks/1/attachments/1/download-link", "", token, "")
	}
	assert.Equal(t, http.StatusUnauthorized, mint("").Code)
	assert.Equal(t, http.StatusUnauthorized, mint(mcpFullToken).Code)
	// The permission to upload does not cover downloading.
	assert.Equal(t, http.StatusUnauthorized, mint(createAttachmentToken(t, uploadPermissions()).Token).Code)
	assert.Equal(t, http.StatusCreated, mint(createAttachmentToken(t, downloadPermissions()).Token).Code)

	shareToken, err := auth.NewLinkShareJWTAuthtoken(&models.LinkSharing{
		ID:          1,
		Hash:        "test",
		ProjectID:   1,
		Permission:  models.PermissionRead,
		SharingType: models.SharingTypeWithoutPassword,
		SharedByID:  1,
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, mint(shareToken).Code)
}

func TestMCP_AttachmentDownload_MintChecksTheAttachment(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	c := newMCPClientOn(t, e, createAttachmentToken(t, downloadPermissions()).Token)

	for name, args := range map[string]map[string]any{
		// Attachment 4 belongs to task 34, not to task 1.
		"attachment of another task": {
			"task":       1,
			"attachment": 4,
		},
		"unknown attachment": {
			"task":       1,
			"attachment": 99999,
		},
		"unknown task": {
			"task":       99999,
			"attachment": 1,
		},
	} {
		res := c.callTool(downloadLinkTool, args)
		assert.Equal(t, true, res["isError"], name)
		assert.Contains(t, toolResultText(t, res), "404", name)
	}

	// Task 34 is not readable by user 1, whatever attachment is asked for.
	res := c.callTool(downloadLinkTool, map[string]any{
		"task":       34,
		"attachment": 4,
	})
	assert.Equal(t, true, res["isError"])
	assert.Contains(t, toolResultText(t, res), "403")
}

// Unlike an upload link, reading is enough to mint.
func TestMCP_AttachmentDownload_MintWorksWithReadAccessOnly(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	s := db.NewSession()
	attachment := &models.TaskAttachment{
		TaskID:      readOnlyTask,
		FileID:      1,
		CreatedByID: 1,
	}
	_, err = s.Insert(attachment)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	c := newMCPClientOn(t, e, createAttachmentToken(t, models.APIPermissions{
		"mcp": {"access"},
		"tasks_attachments": {
			"read_one",
			"create",
		},
	}).Token)
	link := mintDownloadViaMCP(t, c, readOnlyTask, attachment.ID)
	assert.NotEmpty(t, link.DownloadURL)
	res := c.callTool(uploadLinkTool, map[string]any{"task": readOnlyTask})
	assert.Equal(t, true, res["isError"], "the same user still cannot get an upload link")
}

func TestMCP_AttachmentDownload_InvalidTicket(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	for _, ticket := range []string{
		"nope",
		strings.Repeat("a", 43),
	} {
		rec := redeemDownload(t, e, "https://localhost/api/v2/mcp/downloads/"+ticket, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, ticket)
	}
}

func TestMCP_AttachmentDownload_TicketsAreNotInterchangeableWithUploadTickets(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	attachment := uploadAttachmentAs(t, e, 1, "report.txt", []byte("contents"))
	c := newMCPClientOn(t, e, createAttachmentToken(t, models.APIPermissions{
		"mcp": {"access"},
		"tasks_attachments": {
			"create",
			"read_one",
		},
	}).Token)
	download := mintDownloadViaMCP(t, c, 1, attachment)
	upload := mintViaMCP(t, c, 1)

	uploadTicket := upload.UploadURL[strings.LastIndex(upload.UploadURL, "/")+1:]
	downloadTicket := download.DownloadURL[strings.LastIndex(download.DownloadURL, "/")+1:]
	before := attachmentCount(t, 1)

	assert.Equal(t, http.StatusNotFound, redeemDownload(t, e, "https://localhost/api/v2/mcp/downloads/"+uploadTicket, "").Code)
	assert.Equal(t, http.StatusNotFound, redeem(t, e, "https://localhost/api/v2/mcp/uploads/"+downloadTicket, 10).Code)
	assert.Equal(t, before, attachmentCount(t, 1))

	// Neither failed attempt used up the real link.
	assert.Equal(t, http.StatusOK, redeemDownload(t, e, download.DownloadURL, "").Code)
	assert.Equal(t, http.StatusCreated, redeem(t, e, upload.UploadURL, 10).Code)
}

// Expiry is the store evicting the key; the TTL itself is covered by the apiv2 unit test.
func TestMCP_AttachmentDownload_EvictedTicket(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	_, link, _ := mintFreshDownload(t, e, downloadPermissions())
	require.NoError(t, keyvalue.DelPrefix("task_attachment_download_ticket_"))
	assert.Equal(t, http.StatusNotFound, redeemDownload(t, e, link.DownloadURL, "").Code)
}

func TestMCP_AttachmentDownload_RedeemChecksReadAccessAgain(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	attachment := uploadAttachmentAs(t, e, writableSharedTask, "shared.txt", []byte("shared"))
	link := mintDownloadViaMCP(t, newMCPClientOn(t, e, createAttachmentToken(t, downloadPermissions()).Token), writableSharedTask, attachment)

	s := db.NewSession()
	_, err = s.Where("user_id = ? AND project_id = ?", 1, 10).Delete(&models.ProjectUser{})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	rec := redeemDownload(t, e, link.DownloadURL, "")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "shared")
	// The failed attempt used the ticket up.
	assert.Equal(t, http.StatusNotFound, redeemDownload(t, e, link.DownloadURL, "").Code)
}

func TestMCP_AttachmentDownload_RedeemRejectsAGoneAttachment(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	_, link, attachment := mintFreshDownload(t, e, downloadPermissions())

	rec := humaRequest(t, e, http.MethodDelete, "/api/v2/tasks/1/attachments/"+strconv.FormatInt(attachment, 10), "", humaTokenFor(t, &testuser1), "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	assert.Equal(t, http.StatusNotFound, redeemDownload(t, e, link.DownloadURL, "").Code)
}

func TestMCP_AttachmentDownload_RedeemRejectsDeadTokens(t *testing.T) {
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
			token, link, _ := mintFreshDownload(t, e, downloadPermissions())

			kill(t, token)

			rec := redeemDownload(t, e, link.DownloadURL, "")
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "contents")
		})
	}
}

func TestMCP_AttachmentDownload_RedeemRequiresTheReadPermission(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token, link, _ := mintFreshDownload(t, e, downloadPermissions())

	// Tokens cannot be edited through the API, so change the stored scopes directly.
	s := db.NewSession()
	_, err = s.ID(token.ID).Cols("permissions").Update(&models.APIToken{
		APIPermissions: models.APIPermissions{
			"mcp":               {"access"},
			"tasks_attachments": {"create"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	rec := redeemDownload(t, e, link.DownloadURL, "")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "contents")
}

func TestMCP_AttachmentDownload_RedeemRejectsUnavailableOwner(t *testing.T) {
	for name, status := range map[string]user.Status{
		"disabled": user.StatusDisabled,
		"locked":   user.StatusAccountLocked,
		// How an admin freezes an account without disabling it.
		"frozen": user.StatusEmailConfirmationRequired,
	} {
		t.Run(name, func(t *testing.T) {
			e, err := setupTestEnv()
			require.NoError(t, err)
			_, link, _ := mintFreshDownload(t, e, downloadPermissions())

			s := db.NewSession()
			_, err = s.ID(1).Cols("status").Update(&user.User{Status: status})
			require.NoError(t, err)
			require.NoError(t, s.Commit())
			s.Close()

			rec := redeemDownload(t, e, link.DownloadURL, "")
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "contents")
		})
	}
}
