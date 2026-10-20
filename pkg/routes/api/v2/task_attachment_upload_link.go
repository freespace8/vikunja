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
	"fmt"
	"net/http"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web"
	webfiles "code.vikunja.io/api/pkg/web/files"

	"github.com/danielgtaylor/huma/v2"
	"xorm.io/xorm"
)

// MCPUploadsPath is the public redeem route, relative to the v2 group.
const MCPUploadsPath = "/mcp/uploads/"

type taskAttachmentUploadLink struct {
	UploadURL        string    `json:"upload_url" readOnly:"true" doc:"Single-use URL to upload the file(s) to. The URL itself is the credential: any Authorization header is ignored."`
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
		Description: "Returns a single-use upload_url for adding files to this task, valid for 10 minutes. This call does not upload anything: send the local file to upload_url yourself with a plain HTTP multipart POST under the \"files\" field, for example `curl -X POST '<upload_url>' -F 'files=@/path/to/file.png'` (curl_example in the response is ready to run). upload_url needs no Authorization header; one is ignored. The link expires after 10 minutes and is used up by the first upload request that reaches the server's upload handler, even if some or all files in it fail. A request rejected earlier, for example for a wrong field name, an oversized body or a timeout, does not use the link up and can be retried with it. Request a new link if the link is gone. Requires an API token and write access to the task.",
		Method:      http.MethodPost,
		Path:        "/tasks/{task}/attachments/upload-link",
		Tags:        []string{"task"},
	}, taskAttachmentsUploadLink)

	// Public: the ticket in the path is the credential. The path is also listed in
	// unauthenticatedAPIPaths so the token middleware lets it through.
	op := withUploadLimits(huma.Operation{
		OperationID: "mcp-uploads-redeem",
		Summary:     "Upload task attachments with a one-time upload link",
		Description: "Uploads one or more files as attachments to the task the upload link was created for, via multipart/form-data under the \"files\" field. Public: the ticket in the path is the only credential, so no Authorization header is needed and one is ignored. A ticket expires 10 minutes after creation and is used up by the first request that reaches the upload handler; a request rejected earlier (wrong field name, oversized body, timeout) does not use it up. An unknown, expired or used ticket returns 404. The upload runs as the user that created the link, and is refused unless that user still has write access to the task, the API token that created the link still exists, is unexpired and still carries the attachment-create permission, and the user account is still active. Returns the same result as the authenticated upload endpoint, including per-file errors such as exceeding the configured size limit.",
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

func uploadTicketGate(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		exists, err := uploadLinks.peek(ctx.Param("ticket"))
		if err != nil {
			log.Errorf("Could not look up upload ticket: %s", err)
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "could not check the upload link")
			return
		}
		if !exists {
			_ = huma.WriteErr(api, ctx, http.StatusNotFound, uploadLinks.invalidMessage())
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
	caller, err := uploadLinks.linkCallerFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	s := db.NewSession()
	defer s.Close()
	can, err := (&models.TaskAttachment{TaskID: in.TaskID}).CanCreate(s, caller.Auth)
	if err != nil {
		_ = s.Rollback()
		return nil, translateDomainError(err)
	}
	if !can {
		_ = s.Rollback()
		return nil, huma.Error403Forbidden("forbidden")
	}

	ticket, expiresAt, err := uploadLinks.mint(linkTicket{
		UserID:  caller.UserID,
		TokenID: caller.TokenID,
		TaskID:  in.TaskID,
	})
	if err != nil {
		return nil, err
	}

	uploadURL := caller.PublicURL + GroupPrefix + MCPUploadsPath + ticket
	return &taskAttachmentUploadLinkBody{Body: &taskAttachmentUploadLink{
		UploadURL: uploadURL,
		ExpiresAt: expiresAt,
		// #nosec G115 - configured value won't exceed int64 max in practice.
		MaxFileSizeBytes: int64(config.GetMaxFileSizeInMBytes()) * 1024 * 1024,
		CurlExample:      fmt.Sprintf("curl -X POST '%s' -F 'files=@/path/to/file.png'", uploadURL),
	}}, nil
}

func mcpUploadsRedeem(ctx context.Context, in *mcpUploadInput) (*taskAttachmentUploadBody, error) {
	ticket, exists, err := uploadLinks.consume(in.Ticket)
	if err != nil {
		return nil, fmt.Errorf("could not redeem upload ticket: %w", err)
	}
	if !exists {
		return nil, huma.Error404NotFound(uploadLinks.invalidMessage())
	}

	owner, err := uploadLinks.redeemOwner(ticket)
	if err != nil {
		return nil, err
	}

	s := db.NewSession()
	defer s.Close()
	return uploadFormFiles(ctx, s, owner, ticket.TaskID, in.RawBody.Data().Files)
}

// uploadFormFiles commits s; the caller closes it.
func uploadFormFiles(ctx context.Context, s *xorm.Session, a web.Auth, taskID int64, formFiles []huma.FormFile) (*taskAttachmentUploadBody, error) {
	uploads := make([]*models.AttachmentToUpload, 0, len(formFiles))
	for _, file := range formFiles {
		uploads = append(uploads, &models.AttachmentToUpload{
			Reader:   file,
			Filename: file.Filename,
			Size:     uint64(file.Size),
		})
	}

	success, failures, err := models.UploadTaskAttachments(s, a, taskID, uploads)
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}
	if err := s.Commit(); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}
	events.DispatchPending(ctx, s)
	return &taskAttachmentUploadBody{Body: webfiles.BuildUploadResult(success, failures)}, nil
}
