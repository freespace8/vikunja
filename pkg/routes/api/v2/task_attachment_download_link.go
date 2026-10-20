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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web"

	"github.com/danielgtaylor/huma/v2"
)

// MCPDownloadsPath is the public redeem route, relative to the v2 group.
const MCPDownloadsPath = "/mcp/downloads/"

type taskAttachmentDownloadLinkFile struct {
	Name      string `json:"name" readOnly:"true" doc:"The original name of the attachment."`
	Mime      string `json:"mime" readOnly:"true" doc:"The detected mime type of the attachment."`
	SizeBytes uint64 `json:"size_bytes" readOnly:"true" doc:"The size of the attachment in bytes."`
}

type taskAttachmentDownloadLink struct {
	DownloadURL string                         `json:"download_url" readOnly:"true" doc:"Single-use URL to fetch the file from. The URL itself is the credential: any Authorization header is ignored."`
	ExpiresAt   time.Time                      `json:"expires_at" readOnly:"true" doc:"When the link expires, 10 minutes after it was created."`
	File        taskAttachmentDownloadLinkFile `json:"file" readOnly:"true" doc:"What the link downloads."`
	CurlExample string                         `json:"curl_example" readOnly:"true" doc:"A ready-to-run curl command that saves the file to the current directory as vikunja-<attachment id>-<name>, so it cannot overwrite an unrelated local file."`
}

type taskAttachmentDownloadLinkBody struct {
	Body *taskAttachmentDownloadLink
}

// RegisterTaskAttachmentDownloadLinkRoutes is gated like the rest of the attachment routes; the
// check runs here because RegisterAll fires after config loads.
func RegisterTaskAttachmentDownloadLinkRoutes(api huma.API) {
	if !config.ServiceEnableTaskAttachments.GetBool() {
		return
	}

	Register(api, huma.Operation{
		OperationID: "task-attachments-download-link",
		Summary:     "Create a one-time attachment download link",
		Description: "Returns a single-use download_url for one attachment of this task, valid for 10 minutes, together with the file's name, mime type and size. This call does not return the file: fetch download_url yourself with a plain HTTP GET and save it to a local file, for example `curl -fsS -o 'vikunja-12-image.png' '<download_url>'` (curl_example in the response is ready to run), then open that local file. download_url needs no Authorization header; one is ignored. The link expires after 10 minutes and works exactly once: it is used up by the first request that reaches it. Request a new link if the link is gone. Requires an API token and read access to the task. The attachment must belong to the task in the path.",
		Method:      http.MethodPost,
		Path:        "/tasks/{task}/attachments/{attachment}/download-link",
		Tags:        []string{"task"},
	}, taskAttachmentsDownloadLink)

	// Public: the ticket in the path is the credential. The path is also listed in
	// unauthenticatedAPIPaths so the token middleware lets it through.
	Register(api, huma.Operation{
		OperationID: "mcp-downloads-redeem",
		Summary:     "Download a task attachment with a one-time download link",
		Description: "Returns the raw bytes of the attachment the download link was created for, with the same headers as the authenticated download endpoint. Public: the ticket in the path is the only credential, so no Authorization header is needed and one is ignored. A ticket expires 10 minutes after creation and is used up by the first request that reaches it. An unknown, expired or used ticket returns 404, and so does a link whose attachment is gone or that its user can no longer read, whose API token no longer exists, is expired or no longer carries the attachment-read permission, or whose user account is no longer active.",
		Method:      http.MethodGet,
		Path:        MCPDownloadsPath + "{ticket}",
		Tags:        []string{"mcp"},
		Security:    []map[string][]string{},
		Responses:   attachmentBytesResponses(api),
	}, mcpDownloadsRedeem)
}

func init() { AddRouteRegistrar(RegisterTaskAttachmentDownloadLinkRoutes) }

// taskAttachmentsDownloadLink owns auth and the permission check because there is no
// handler.Do* for it (see the api-v2-routes skill's "Non-CRUDable / custom routes" section).
func taskAttachmentsDownloadLink(ctx context.Context, in *struct {
	TaskID       int64 `path:"task" doc:"The id of the task the attachment belongs to."`
	AttachmentID int64 `path:"attachment" doc:"The id of the attachment to create a download link for."`
}) (*taskAttachmentDownloadLinkBody, error) {
	caller, err := downloadLinks.linkCallerFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	// Checks read access and that the attachment belongs to the task.
	attachment, err := models.GetTaskAttachmentInfo(caller.Auth, in.TaskID, in.AttachmentID)
	if err != nil {
		return nil, translateDomainError(err)
	}

	ticket, expiresAt, err := downloadLinks.mint(linkTicket{
		UserID:       caller.UserID,
		TokenID:      caller.TokenID,
		TaskID:       in.TaskID,
		AttachmentID: in.AttachmentID,
	})
	if err != nil {
		return nil, err
	}

	downloadURL := caller.PublicURL + GroupPrefix + MCPDownloadsPath + ticket
	return &taskAttachmentDownloadLinkBody{Body: &taskAttachmentDownloadLink{
		DownloadURL: downloadURL,
		ExpiresAt:   expiresAt,
		File: taskAttachmentDownloadLinkFile{
			Name:      attachment.File.Name,
			Mime:      attachment.File.Mime,
			SizeBytes: attachment.File.Size,
		},
		CurlExample: fmt.Sprintf("curl -fsS -o %s %s", shellQuote(localDownloadName(attachment.ID, attachment.File.Name)), shellQuote(downloadURL)),
	}}, nil
}

func mcpDownloadsRedeem(_ context.Context, in *struct {
	Ticket string `path:"ticket" doc:"The one-time ticket from a download link."`
}) (*huma.StreamResponse, error) {
	ticket, exists, err := downloadLinks.consume(in.Ticket)
	if err != nil {
		return nil, fmt.Errorf("could not redeem download ticket: %w", err)
	}
	if !exists {
		return nil, huma.Error404NotFound(downloadLinks.invalidMessage())
	}

	owner, err := downloadLinks.redeemOwner(ticket)
	if err != nil {
		return nil, err
	}

	file, err := models.GetTaskAttachmentForDownload(owner, ticket.TaskID, ticket.AttachmentID)
	if err != nil {
		// Whatever refused the owner, the caller only learns that the link is no good.
		var hp web.HTTPErrorProcessor
		if errors.As(err, &hp) && hp.HTTPError().HTTPCode < http.StatusInternalServerError {
			return nil, huma.Error404NotFound(downloadLinks.invalidMessage())
		}
		return nil, fmt.Errorf("could not open the attachment of a download ticket: %w", err)
	}
	return streamAttachmentFile(file), nil
}

// maxDownloadNameBytes keeps the local name, prefix included, under the usual 255-byte limit.
const maxDownloadNameBytes = 200

// localDownloadName prefixes the attachment id so a collaborator-chosen name like ".bashrc" or
// "Makefile" cannot overwrite an unrelated file in the agent's working directory.
func localDownloadName(attachmentID int64, name string) string {
	return fmt.Sprintf("vikunja-%d-%s", attachmentID, safeDownloadName(name))
}

// Attachment names are user-controlled and end up in a shell command: keep only the last path
// element, drop control and format characters (such as bidi overrides), cap the length, and
// avoid names curl would read as stdout ("-") or as an output-name template ("#1").
func safeDownloadName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		case r == '#':
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(truncateUTF8(name, maxDownloadNameBytes))
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	if strings.HasPrefix(name, "-") {
		return "_" + name
	}
	return name
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// truncateUTF8 cuts s to at most n bytes without splitting a character.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
