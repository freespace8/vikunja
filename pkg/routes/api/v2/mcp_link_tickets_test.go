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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/modules/keyvalue"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkTickets(t *testing.T) {
	keyvalue.InitStorage()
	for _, purpose := range []linkPurpose{
		uploadLinks,
		downloadLinks,
	} {
		t.Run(purpose.name, func(t *testing.T) {
			testLinkTickets(t, purpose)
		})
	}
}

func testLinkTickets(t *testing.T, purpose linkPurpose) {
	want := linkTicket{
		UserID:       1,
		TokenID:      12,
		TaskID:       3,
		AttachmentID: 4,
	}

	t.Run("single use", func(t *testing.T) {
		ticket, expiresAt, err := purpose.mint(want)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(ticket), 43, "32 random bytes, base64url encoded")
		assert.NotContains(t, ticket, "=")
		assert.WithinDuration(t, time.Now().Add(10*time.Minute), expiresAt, time.Minute)

		exists, err := purpose.peek(ticket)
		require.NoError(t, err)
		assert.True(t, exists, "peeking must not consume the ticket")

		got, exists, err := purpose.consume(ticket)
		require.NoError(t, err)
		require.True(t, exists)
		assert.Equal(t, want, got)

		_, exists, err = purpose.consume(ticket)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("concurrent redemption has a single winner", func(t *testing.T) {
		ticket, _, err := purpose.mint(want)
		require.NoError(t, err)

		const redeemers = 20
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range redeemers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, exists, err := purpose.consume(ticket)
				assert.NoError(t, err)
				if exists {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, int32(1), wins.Load())
	})

	t.Run("the raw ticket is not a store key", func(t *testing.T) {
		ticket, _, err := purpose.mint(want)
		require.NoError(t, err)
		for _, prefix := range []string{purpose.ticketKeyPrefix, purpose.claimKeyPrefix} {
			keys, err := keyvalue.ListKeys(prefix)
			require.NoError(t, err)
			for _, key := range keys {
				assert.NotContains(t, key, ticket, key)
			}
		}
		keys, err := keyvalue.ListKeys(purpose.ticketKeyPrefix)
		require.NoError(t, err)
		require.NotEmpty(t, keys)
	})

	t.Run("expires", func(t *testing.T) {
		previous := linkTicketTTL
		linkTicketTTL = 50 * time.Millisecond
		t.Cleanup(func() { linkTicketTTL = previous })

		ticket, _, err := purpose.mint(want)
		require.NoError(t, err)
		time.Sleep(150 * time.Millisecond)
		_, exists, err := purpose.consume(ticket)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("unknown", func(t *testing.T) {
		_, exists, err := purpose.consume("nope")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("tickets are unique", func(t *testing.T) {
		a, _, err := purpose.mint(want)
		require.NoError(t, err)
		b, _, err := purpose.mint(want)
		require.NoError(t, err)
		assert.NotEqual(t, a, b)
	})
}

func TestLinkTicketsAreNotInterchangeable(t *testing.T) {
	keyvalue.InitStorage()
	ticket := linkTicket{
		UserID:  1,
		TokenID: 12,
		TaskID:  3,
	}

	for _, tc := range []struct {
		name         string
		from, redeem linkPurpose
	}{
		{
			name:   "upload ticket at the download store",
			from:   uploadLinks,
			redeem: downloadLinks,
		},
		{
			name:   "download ticket at the upload store",
			from:   downloadLinks,
			redeem: uploadLinks,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			minted, _, err := tc.from.mint(ticket)
			require.NoError(t, err)

			exists, err := tc.redeem.peek(minted)
			require.NoError(t, err)
			assert.False(t, exists)
			_, exists, err = tc.redeem.consume(minted)
			require.NoError(t, err)
			assert.False(t, exists)

			// The failed attempt at the other store must not have used it up.
			_, exists, err = tc.from.consume(minted)
			require.NoError(t, err)
			assert.True(t, exists)
		})
	}
}
