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

package apiv2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/humabridge"
	"code.vikunja.io/api/pkg/modules/keyvalue"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"
	webfiles "code.vikunja.io/api/pkg/web/files"

	"github.com/danielgtaylor/huma/v2"
	"xorm.io/xorm"
)

// MCP calls carry JSON only, so an MCP client cannot send a file through a tool call. Instead it
// mints a short-lived, single-use upload link and then uploads the file with plain HTTP. The
// ticket in the link is the only credential the upload route checks.

const (
	uploadTicketKeyPrefix = "task_attachment_upload_ticket_"
	uploadTicketBytes     = 32
	// MCPUploadsPath is the public redeem route, relative to the v2 group.
	MCPUploadsPath = "/mcp/uploads/"
)

// A var so tests can shorten it.
var uploadTicketTTL = 10 * time.Minute

type uploadTicket struct {
	UserID int64
	// 0 when the link was minted with a session instead of an API token.
	TokenID int64
	TaskID  int64
}

// The store has no atomic get-and-delete, so this serialises redemptions within one process.
var uploadTicketMu sync.Mutex

// Only a hash of the ticket is stored, so reading the store does not reveal usable links.
func uploadTicketKey(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return uploadTicketKeyPrefix + hex.EncodeToString(sum[:])
}

func mintUploadTicket(t uploadTicket) (ticket string, expiresAt time.Time, err error) {
	raw := make([]byte, uploadTicketBytes)
	if _, err = rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	ticket = base64.RawURLEncoding.EncodeToString(raw)
	expiresAt = time.Now().Add(uploadTicketTTL)
	return ticket, expiresAt, keyvalue.PutWithTTL(uploadTicketKey(ticket), t, uploadTicketTTL)
}

func peekUploadTicket(ticket string) (exists bool, err error) {
	var t uploadTicket
	return keyvalue.GetWithValue(uploadTicketKey(ticket), &t)
}

// consumeUploadTicket removes the ticket whatever happens next, so a failed redemption cannot be retried.
func consumeUploadTicket(ticket string) (t uploadTicket, exists bool, err error) {
	uploadTicketMu.Lock()
	defer uploadTicketMu.Unlock()
	key := uploadTicketKey(ticket)
	exists, err = keyvalue.GetWithValue(key, &t)
	if err != nil || !exists {
		return uploadTicket{}, false, err
	}
	return t, true, keyvalue.Del(key)
}

type taskAttachmentUploadLink struct {
	UploadURL        string    `json:"upload_url" readOnly:"true" doc:"Single-use URL to upload the file(s) to. The URL itself is the credential: no Authorization header is needed or accepted."`
	ExpiresAt        time.Time `json:"expires_at" readOnly:"true" doc:"When the link expires, 10 minutes after it was created."`
	MaxFileSizeBytes int64     `json:"max_file_size_bytes" readOnly:"true" doc:"Largest file the server accepts per file, in bytes."`
	CurlExample      string    `json:"curl_example" readOnly:"true" doc:"A ready-to-run curl command; replace /path/to/file.png with the local file."`
}

type taskAttachmentUploadLinkBody struct {
	Body *taskAttachmentUploadLink
}

type mcpUploadInput struct {
	Ticket string `path:"ticket" doc:"The one-time ticket from an upload link."`
	// Same shape as the authenticated upload endpoint.
	RawBody huma.MultipartFormFiles[struct {
		Files []huma.FormFile `form:"files" required:"true" doc:"One or more files to upload as task attachments. Send multiple parts under the same \"files\" field to upload several at once."`
	}]
}

// RegisterTaskAttachmentUploadLinkRoutes is gated like the rest of the attachment routes; the
// check runs here because RegisterAll fires after config loads.
func RegisterTaskAttachmentUploadLinkRoutes(api huma.API) {
	if !config.ServiceEnableTaskAttachments.GetBool() {
		return
	}

	Register(api, huma.Operation{
		OperationID: "task-attachments-upload-link",
		Summary:     "Create a one-time attachment upload link",
		Description: "Returns a single-use upload_url for adding files to this task, valid for 10 minutes. This call does not upload anything: send the local file to upload_url yourself with a plain HTTP multipart POST under the \"files\" field, for example `curl -X POST '<upload_url>' -F 'files=@/path/to/file.png'` (curl_example in the response is ready to run). Do not send an Authorization header to upload_url. The link stops working after one upload attempt or when it expires; request a new one if the upload fails. Requires write access to the task.",
		Method:      http.MethodPost,
		Path:        "/tasks/{task}/attachments/upload-link",
		Tags:        []string{"task"},
	}, taskAttachmentsUploadLink)

	// Public: the ticket in the path is the credential. The path is also listed in
	// unauthenticatedAPIPaths so the token middleware lets it through.
	op := withUploadLimits(huma.Operation{
		OperationID: "mcp-uploads-redeem",
		Summary:     "Upload task attachments with a one-time upload link",
		Description: "Uploads one or more files as attachments to the task the upload link was created for, via multipart/form-data under the \"files\" field. Public: the ticket in the path is the only credential, so send no Authorization header. A ticket works for one request and expires 10 minutes after creation; an unknown, expired or used ticket returns 404. The upload runs with the permissions of the user (and API token) that created the link, checked again now. Returns the same result as the authenticated upload endpoint, including per-file errors such as exceeding the configured size limit.",
		Method:      http.MethodPost,
		Path:        MCPUploadsPath + "{ticket}",
		Tags:        []string{"mcp"},
		Security:    []map[string][]string{},
	})
	// Rejects unknown tickets before Huma reads a body that may be as large as the file size limit.
	op.Middlewares = append(op.Middlewares, uploadTicketGate(api))
	Register(api, op, mcpUploadsRedeem)
}

func init() { AddRouteRegistrar(RegisterTaskAttachmentUploadLinkRoutes) }

const errUploadTicketInvalid = "upload link is invalid, expired or already used"

func uploadTicketGate(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		exists, err := peekUploadTicket(ctx.Param("ticket"))
		if err != nil || !exists {
			_ = huma.WriteErr(api, ctx, http.StatusNotFound, errUploadTicketInvalid)
			return
		}
		next(ctx)
	}
}

// taskAttachmentsUploadLink owns auth, the session and the permission check because there is no
// handler.Do* for it (see the api-v2-routes skill's "Non-CRUDable / custom routes" section).
func taskAttachmentsUploadLink(ctx context.Context, in *struct {
	TaskID int64 `path:"task" doc:"The id of the task to create an upload link for."`
}) (*taskAttachmentUploadLinkBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	// The link is redeemed without a request context, so it must name a real user (not a link share).
	u, err := user.GetFromAuth(a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	publicURL := strings.TrimRight(config.ServicePublicURL.GetString(), "/")
	if publicURL == "" {
		return nil, huma.Error503ServiceUnavailable("service.publicurl is not configured, so an upload link cannot be built")
	}

	s := db.NewSession()
	defer s.Close()
	can, err := (&models.TaskAttachment{TaskID: in.TaskID}).CanCreate(s, a)
	if err != nil {
		_ = s.Rollback()
		return nil, translateDomainError(err)
	}
	if !can {
		_ = s.Rollback()
		return nil, huma.Error403Forbidden("forbidden")
	}

	var tokenID int64
	if ec := humabridge.EchoContextFrom(ctx); ec != nil {
		if token, ok := (*ec).Get("api_token").(*models.APIToken); ok && token != nil {
			tokenID = token.ID
		}
	}
	ticket, expiresAt, err := mintUploadTicket(uploadTicket{UserID: u.ID, TokenID: tokenID, TaskID: in.TaskID})
	if err != nil {
		return nil, fmt.Errorf("could not store upload ticket: %w", err)
	}

	uploadURL := publicURL + GroupPrefix + MCPUploadsPath + ticket
	return &taskAttachmentUploadLinkBody{Body: &taskAttachmentUploadLink{
		UploadURL: uploadURL,
		ExpiresAt: expiresAt,
		// #nosec G115 - configured value won't exceed int64 max in practice.
		MaxFileSizeBytes: int64(config.GetMaxFileSizeInMBytes()) * 1024 * 1024,
		CurlExample:      fmt.Sprintf("curl -X POST '%s' -F 'files=@/path/to/file.png'", uploadURL),
	}}, nil
}

func mcpUploadsRedeem(_ context.Context, in *mcpUploadInput) (*taskAttachmentUploadBody, error) {
	ticket, exists, err := consumeUploadTicket(in.Ticket)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, huma.Error404NotFound(errUploadTicketInvalid)
	}

	s := db.NewSession()
	defer s.Close()

	// The link outlives neither its token nor its owner.
	if ticket.TokenID != 0 {
		token, err := models.GetAPITokenByID(s, ticket.TokenID)
		if err != nil {
			_ = s.Rollback()
			return nil, err
		}
		if token.ID == 0 || token.OwnerID != ticket.UserID || time.Now().After(token.ExpiresAt) {
			_ = s.Rollback()
			return nil, huma.Error404NotFound(errUploadTicketInvalid)
		}
	}
	owner, err := user.GetUserByID(s, ticket.UserID)
	if err != nil {
		_ = s.Rollback()
		if user.IsErrUserStatusError(err) || user.IsErrUserDoesNotExist(err) {
			return nil, huma.Error404NotFound(errUploadTicketInvalid)
		}
		return nil, err
	}

	return uploadFormFiles(s, owner, ticket.TaskID, in.RawBody.Data().Files)
}

// uploadFormFiles commits s; the caller closes it.
func uploadFormFiles(s *xorm.Session, a web.Auth, taskID int64, formFiles []huma.FormFile) (*taskAttachmentUploadBody, error) {
	uploads := make([]*models.AttachmentToUpload, 0, len(formFiles))
	for _, file := range formFiles {
		uploads = append(uploads, &models.AttachmentToUpload{Reader: file, Filename: file.Filename, Size: uint64(file.Size)})
	}

	success, failures, err := models.UploadTaskAttachments(s, a, taskID, uploads)
	if err != nil {
		_ = s.Rollback()
		return nil, translateDomainError(err)
	}
	if err := s.Commit(); err != nil {
		_ = s.Rollback()
		return nil, translateDomainError(err)
	}
	return &taskAttachmentUploadBody{Body: webfiles.BuildUploadResult(success, failures)}, nil
}
