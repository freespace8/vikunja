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
	"testing"
	"time"

	"code.vikunja.io/api/pkg/modules/keyvalue"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadTicket(t *testing.T) {
	keyvalue.InitStorage()
	want := uploadTicket{
		UserID:  1,
		TokenID: 12,
		TaskID:  3,
	}

	t.Run("single use", func(t *testing.T) {
		ticket, expiresAt, err := mintUploadTicket(want)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(ticket), 43, "32 random bytes, base64url encoded")
		assert.NotContains(t, ticket, "=")
		assert.WithinDuration(t, time.Now().Add(10*time.Minute), expiresAt, time.Minute)

		exists, err := peekUploadTicket(ticket)
		require.NoError(t, err)
		assert.True(t, exists, "peeking must not consume the ticket")

		got, exists, err := consumeUploadTicket(ticket)
		require.NoError(t, err)
		require.True(t, exists)
		assert.Equal(t, want, got)

		_, exists, err = consumeUploadTicket(ticket)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("the raw ticket is not a store key", func(t *testing.T) {
		ticket, _, err := mintUploadTicket(want)
		require.NoError(t, err)
		keys, err := keyvalue.ListKeys(uploadTicketKeyPrefix)
		require.NoError(t, err)
		require.NotEmpty(t, keys)
		for _, key := range keys {
			assert.NotContains(t, key, ticket, key)
		}
	})

	t.Run("expires", func(t *testing.T) {
		previous := uploadTicketTTL
		uploadTicketTTL = 50 * time.Millisecond
		t.Cleanup(func() { uploadTicketTTL = previous })

		ticket, _, err := mintUploadTicket(want)
		require.NoError(t, err)
		time.Sleep(150 * time.Millisecond)
		_, exists, err := consumeUploadTicket(ticket)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("unknown", func(t *testing.T) {
		_, exists, err := consumeUploadTicket("nope")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("tickets are unique", func(t *testing.T) {
		a, _, err := mintUploadTicket(want)
		require.NoError(t, err)
		b, _, err := mintUploadTicket(want)
		require.NoError(t, err)
		assert.NotEqual(t, a, b)
	})
}
