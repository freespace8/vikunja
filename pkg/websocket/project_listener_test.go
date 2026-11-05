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

package websocket

import (
	"testing"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures: project 3 is owned by user 3 and shared with users 1 and 2 (team 1
// holds users 1 and 2). Project 40 is a child of project 3 owned by user 1.
// Project 1 is owned by user 1 and has view 1.
func TestProjectChangeListener(t *testing.T) {
	t.Run("pushes a task update to every user who can read the project", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conns := map[int64]*Connection{}
		for _, userID := range []int64{1, 2, 3} {
			conns[userID] = projectChangeConn(userID)
			GetHub().Register(conns[userID])
		}

		event := &models.TaskUpdatedEvent{Task: &models.Task{ID: 42, ProjectID: 3}}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		for userID, conn := range conns {
			require.Len(t, conn.send, 1, "user %d should be notified", userID)
			msg := <-conn.send
			assert.Equal(t, "project.changed", msg.Event)
			payload, ok := msg.Data.(projectChangedPayload)
			require.True(t, ok, "payload must be the id-only change")
			assert.Equal(t, int64(3), payload.ProjectID)
			assert.Equal(t, []int64{3}, payload.ProjectIDs)
			assert.Equal(t, int64(42), payload.TaskID)
		}
	})

	t.Run("does not push to users without access", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(5)
		GetHub().Register(conn)

		event := &models.TaskUpdatedEvent{Task: &models.Task{ID: 42, ProjectID: 3}}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		assert.Empty(t, conn.send)
	})

	t.Run("does not push to connections that did not subscribe", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := &Connection{
			userID:        1,
			subscriptions: map[string]bool{"notification.created": true},
			send:          make(chan OutgoingMessage, 16),
		}
		GetHub().Register(conn)

		event := &models.TaskUpdatedEvent{Task: &models.Task{ID: 42, ProjectID: 3}}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		assert.Empty(t, conn.send)
	})

	t.Run("a sub-project change reaches the parent's users and lists the ancestors", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(2)
		GetHub().Register(conn)

		event := &models.TaskUpdatedEvent{Task: &models.Task{ID: 42, ProjectID: 40}}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, int64(40), payload.ProjectID)
		assert.ElementsMatch(t, []int64{40, 3}, payload.ProjectIDs)
	})

	t.Run("resolves the project of a positions recalculated event", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(1)
		GetHub().Register(conn)

		event := &models.TaskPositionsRecalculatedEvent{
			NewTaskPositions: []*models.TaskPosition{{TaskID: 1, ProjectViewID: 1, Position: 12}},
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, int64(1), payload.ProjectID)
		assert.Equal(t, []int64{1}, payload.ProjectIDs)
	})

	t.Run("a purged project still notifies the users of its parent", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(2)
		GetHub().Register(conn)

		// Project 999 no longer exists; the event's parent id is the only way to its ancestors.
		parent := int64(3)
		event := &models.ProjectPurgedEvent{Project: &models.Project{ID: 999, ParentProjectID: &parent}}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, int64(999), payload.ProjectID)
		assert.ElementsMatch(t, []int64{999, 3}, payload.ProjectIDs)
		assert.Equal(t, int64(0), payload.TaskID)
	})
}

func projectChangeConn(userID int64) *Connection {
	return &Connection{
		userID:        userID,
		subscriptions: map[string]bool{"project.changed": true},
		send:          make(chan OutgoingMessage, 16),
	}
}
