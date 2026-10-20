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
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/humabridge"
	"code.vikunja.io/api/pkg/modules/keyvalue"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/utils"
	"code.vikunja.io/api/pkg/web"

	"github.com/danielgtaylor/huma/v2"
)

// MCP calls carry JSON only, so an MCP client cannot move file bytes through a tool call. Instead
// it mints a short-lived, single-use link and then sends or fetches the file with plain HTTP. The
// ticket in the link is the only credential the redeem route checks. Upload and download links
// share this store; each purpose has its own key prefixes, so a ticket of one kind never redeems
// the other.

const linkTicketBytes = 32

// A var so tests can shorten it.
var linkTicketTTL = 10 * time.Minute

// linkPurpose describes one kind of link and the API token permission that governs minting and
// redeeming it.
type linkPurpose struct {
	name            string
	ticketKeyPrefix string
	claimKeyPrefix  string
	tokenGroup      string
	tokenPermission string
}

var uploadLinks = linkPurpose{
	name:            "upload",
	ticketKeyPrefix: "task_attachment_upload_ticket_",
	claimKeyPrefix:  "task_attachment_upload_claim_",
	tokenGroup:      "tasks_attachments",
	tokenPermission: "create",
}

var downloadLinks = linkPurpose{
	name:            "download",
	ticketKeyPrefix: "task_attachment_download_ticket_",
	claimKeyPrefix:  "task_attachment_download_claim_",
	tokenGroup:      "tasks_attachments",
	tokenPermission: "read_one",
}

// linkTicket is what a ticket is bound to. AttachmentID is only set for download links.
type linkTicket struct {
	UserID       int64
	TokenID      int64
	TaskID       int64
	AttachmentID int64
}

func (p linkPurpose) invalidMessage() string {
	return p.name + " link is invalid, expired or already used"
}

// Only a hash of the ticket is stored, so reading the store does not reveal usable links.
func (p linkPurpose) ticketKey(ticket string) string {
	return p.ticketKeyPrefix + utils.Sha256Hex(ticket)
}

func (p linkPurpose) claimKey(ticket string) string {
	return p.claimKeyPrefix + utils.Sha256Hex(ticket)
}

func (p linkPurpose) mint(t linkTicket) (ticket string, expiresAt time.Time, err error) {
	raw, err := utils.CryptoRandomBytes(linkTicketBytes)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("could not generate %s ticket: %w", p.name, err)
	}
	ticket = base64.RawURLEncoding.EncodeToString(raw)
	expiresAt = time.Now().Add(linkTicketTTL)
	if err = keyvalue.PutWithTTL(p.ticketKey(ticket), t, linkTicketTTL); err != nil {
		return "", time.Time{}, fmt.Errorf("could not store %s ticket: %w", p.name, err)
	}
	return ticket, expiresAt, nil
}

func (p linkPurpose) peek(ticket string) (exists bool, err error) {
	var t linkTicket
	return keyvalue.GetWithValue(p.ticketKey(ticket), &t)
}

// consume removes the ticket whatever happens next, so a redemption that got this far cannot be retried.
// Only the request whose increment of the claim counter returns 1 wins, which the store does atomically,
// so single use also holds across instances sharing a Redis store.
func (p linkPurpose) consume(ticket string) (t linkTicket, exists bool, err error) {
	exists, err = keyvalue.GetWithValue(p.ticketKey(ticket), &t)
	if err != nil || !exists {
		return linkTicket{}, false, err
	}
	claims, err := keyvalue.IncrByWithTTL(p.claimKey(ticket), 1, linkTicketTTL)
	if err != nil {
		return linkTicket{}, false, fmt.Errorf("could not claim %s ticket: %w", p.name, err)
	}
	if claims != 1 {
		return linkTicket{}, false, nil
	}
	if err := keyvalue.Del(p.ticketKey(ticket)); err != nil {
		return linkTicket{}, false, fmt.Errorf("could not remove claimed %s ticket: %w", p.name, err)
	}
	return t, true, nil
}

// linkCaller is who asks for a link: the authenticated user, the API token they used and the
// public base URL the link is built on.
type linkCaller struct {
	Auth      web.Auth
	UserID    int64
	TokenID   int64
	PublicURL string
}

// linkCallerFromCtx refuses everything but an API token owned by a real user: the link is redeemed
// without a request context, so it cannot name a link share or a session.
func (p linkPurpose) linkCallerFromCtx(ctx context.Context) (*linkCaller, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	u, err := user.GetFromAuth(a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	var tokenID int64
	if ec := humabridge.EchoContextFrom(ctx); ec != nil {
		if token, ok := (*ec).Get("api_token").(*models.APIToken); ok && token != nil {
			tokenID = token.ID
		}
	}
	if tokenID == 0 {
		return nil, huma.Error403Forbidden(p.name + " links can only be created with an API token")
	}
	publicURL := strings.TrimRight(config.ServicePublicURL.GetString(), "/")
	if publicURL == "" {
		return nil, huma.Error503ServiceUnavailable("service.publicurl is not configured, so " + p.name + " links cannot be built")
	}
	return &linkCaller{
		Auth:      a,
		UserID:    u.ID,
		TokenID:   tokenID,
		PublicURL: publicURL,
	}, nil
}

// redeemOwner checks that the ticket outlives neither its token nor its owner. Any failure is a
// 404 with the generic message, so a probe learns nothing about which check refused it.
func (p linkPurpose) redeemOwner(t linkTicket) (*user.User, error) {
	s := db.NewSession()
	defer s.Close()

	token, err := models.GetAPITokenByID(s, t.TokenID)
	if err != nil {
		_ = s.Rollback()
		if models.IsErrAPITokenDoesNotExist(err) {
			return nil, huma.Error404NotFound(p.invalidMessage())
		}
		return nil, fmt.Errorf("could not load the token of the %s ticket: %w", p.name, err)
	}
	if token.OwnerID != t.UserID || time.Now().After(token.ExpiresAt) || !token.HasPermission(p.tokenGroup, p.tokenPermission) {
		_ = s.Rollback()
		return nil, huma.Error404NotFound(p.invalidMessage())
	}
	// The same gate token authentication applies, which also refuses an account an admin froze.
	owner, err := user.GetUserByIDForAuth(s, t.UserID)
	if err != nil {
		_ = s.Rollback()
		if user.IsErrUserCannotAuthenticate(err) || user.IsErrUserDoesNotExist(err) {
			return nil, huma.Error404NotFound(p.invalidMessage())
		}
		return nil, fmt.Errorf("could not load the owner of the %s ticket: %w", p.name, err)
	}
	return owner, nil
}
