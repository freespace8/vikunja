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
	"context"
	"testing"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 夹具：项目 3 属于用户 3，通过团队 1 共享给用户 1 和 2；项目 40 是项目 3 的子项目，属于用户 1；
// 项目 1 属于用户 1，只有他自己能读。
func TestProjectChangeListener(t *testing.T) {
	t.Run("pushes a task update to every user who can read the project", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conns := map[int64]*Connection{}
		for _, userID := range []int64{1, 2, 3} {
			conns[userID] = projectChangeConn(userID)
			GetHub().Register(conns[userID])
		}

		event := &models.TaskUpdatedEvent{
			Task: taskInProject(42, 3),
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		for userID, conn := range conns {
			require.Len(t, conn.send, 1, "user %d should be notified", userID)
			msg := <-conn.send
			assert.Equal(t, "project.changed", msg.Event)
			payload, ok := msg.Data.(projectChangedPayload)
			require.True(t, ok, "payload must be the id-only change")
			assert.Equal(t, []int64{3}, payload.ProjectIDs)
			assert.Equal(t, int64(42), payload.TaskID)
			assert.Equal(t, projectChangeLayout, payload.Kind)
		}
	})

	t.Run("does not push to users without access", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(5)
		GetHub().Register(conn)

		event := &models.TaskUpdatedEvent{
			Task: taskInProject(42, 3),
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		assert.Empty(t, conn.send)
	})

	t.Run("does not push to connections that did not subscribe", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := &Connection{
			userID: 1,
			subscriptions: map[string]bool{
				"notification.created": true,
			},
			send: make(chan OutgoingMessage, 16),
		}
		GetHub().Register(conn)

		event := &models.TaskUpdatedEvent{
			Task: taskInProject(42, 3),
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		assert.Empty(t, conn.send)
	})

	t.Run("a sub-project change reaches the parent's users and lists the ancestors", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(2)
		GetHub().Register(conn)

		event := &models.TaskUpdatedEvent{
			Task: taskInProject(42, 40),
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.ElementsMatch(t, []int64{40, 3}, payload.ProjectIDs)
	})

	t.Run("a comment only marks the task content", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(1)
		GetHub().Register(conn)

		event := &models.TaskCommentCreatedEvent{
			Task: taskInProject(42, 3),
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, projectChangeContent, payload.Kind)
		assert.Equal(t, int64(42), payload.TaskID)
		assert.Equal(t, []int64{3}, payload.ProjectIDs)
	})

	t.Run("a batch create notifies each project once", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(1)
		GetHub().Register(conn)

		event := &models.TasksBatchCreatedEvent{
			Tasks: []*models.Task{
				taskInProject(1, 3),
				taskInProject(2, 3),
				taskInProject(3, 40),
			},
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, projectChangeLayout, payload.Kind)
		assert.Equal(t, int64(0), payload.TaskID)
		assert.ElementsMatch(t, []int64{3, 40}, payload.ProjectIDs)
	})

	t.Run("a task moved to another project reaches the new project's users", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		// 用户 3 能读项目 3，但任务移入的项目 1 对他不可见。
		conn := projectChangeConn(3)
		GetHub().Register(conn)

		event := &models.TaskUpdatedEvent{
			Task: taskInProject(42, 1),
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		assert.Empty(t, conn.send)
	})

	t.Run("trashing a project reaches its own users and the parent's", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		conns := map[int64]*Connection{}
		for _, userID := range []int64{1, 2} {
			conns[userID] = projectChangeConn(userID)
			GetHub().Register(conns[userID])
		}

		require.NoError(t, (&models.Project{ID: 40}).Delete(s, &user.User{ID: 1}))
		require.NoError(t, s.Commit())
		events.ClearDispatchedEvents()
		events.DispatchPending(context.Background(), s)

		deleted := events.GetDispatchedEvents((&models.ProjectDeletedEvent{}).Name())
		require.Len(t, deleted, 1)
		events.TestListener(t, deleted[0], &ProjectChangeListener{wsEvent: deleted[0].Name()})

		// 用户 1 是项目 40 的拥有者，用户 2 通过父项目 3 的共享读到它。
		for userID, conn := range conns {
			require.Len(t, conn.send, 1, "user %d should be notified", userID)
			payload, ok := (<-conn.send).Data.(projectChangedPayload)
			require.True(t, ok)
			assert.Equal(t, projectChangeStructure, payload.Kind)
			assert.ElementsMatch(t, []int64{40, 3}, payload.ProjectIDs)
		}
	})

	t.Run("trashing a top-level project reaches its owner", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		conn := projectChangeConn(1)
		GetHub().Register(conn)

		require.NoError(t, (&models.Project{ID: 1}).Delete(s, &user.User{ID: 1}))
		require.NoError(t, s.Commit())
		events.ClearDispatchedEvents()
		events.DispatchPending(context.Background(), s)

		deleted := events.GetDispatchedEvents((&models.ProjectDeletedEvent{}).Name())
		require.Len(t, deleted, 1)
		events.TestListener(t, deleted[0], &ProjectChangeListener{wsEvent: deleted[0].Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, []int64{1}, payload.ProjectIDs)
	})

	t.Run("a user shared only on the sub-project does not get the parent id", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		conn := projectChangeConn(5)
		GetHub().Register(conn)
		_, err := s.Insert(&models.ProjectUser{
			UserID:     5,
			ProjectID:  40,
			Permission: models.PermissionRead,
		})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		event := &models.TaskUpdatedEvent{
			Task: taskInProject(42, 40),
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, []int64{40}, payload.ProjectIDs)
	})

	t.Run("a reparented project reaches the users of the old parent", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		conn := projectChangeConn(5)
		GetHub().Register(conn)
		// 用户 5 只通过父项目 41 拿到权限，新的父项目 7 对他不可见。
		_, err := s.Insert(&models.ProjectUser{
			UserID:     5,
			ProjectID:  41,
			Permission: models.PermissionRead,
		})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		project, err := models.GetProjectSimpleByID(s, 42)
		require.NoError(t, err)
		newParent := int64(7)
		project.ParentProjectID = &newParent
		require.NoError(t, project.Update(s, &user.User{ID: 6}))
		require.NoError(t, s.Commit())
		events.ClearDispatchedEvents()
		events.DispatchPending(context.Background(), s)

		changed := events.GetDispatchedEvents((&models.ProjectParentChangedEvent{}).Name())
		require.Len(t, changed, 1)
		events.TestListener(t, changed[0], &ProjectChangeListener{wsEvent: changed[0].Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, projectChangeStructure, payload.Kind)
		// 换父项目后用户 5 读不到项目 42，只剩旧父项目要刷新。
		assert.Equal(t, []int64{41}, payload.ProjectIDs)
	})

	t.Run("a bucket change notifies the project's users", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(2)
		GetHub().Register(conn)

		event := &models.BucketCreatedEvent{
			Bucket: &models.Bucket{
				ID:            7,
				ProjectViewID: 4,
			},
			ProjectID: 40,
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, projectChangeStructure, payload.Kind)
		assert.ElementsMatch(t, []int64{40, 3}, payload.ProjectIDs)
	})

	t.Run("a project view change notifies the project's users", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(2)
		GetHub().Register(conn)

		event := &models.ProjectViewUpdatedEvent{
			View: &models.ProjectView{
				ID:        5,
				ProjectID: 3,
			},
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, projectChangeStructure, payload.Kind)
		assert.ElementsMatch(t, []int64{3}, payload.ProjectIDs)
	})

	t.Run("resolves the project of a positions recalculated event", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(1)
		GetHub().Register(conn)

		event := &models.TaskPositionsRecalculatedEvent{
			NewTaskPositions: []*models.TaskPosition{
				{
					TaskID:        1,
					ProjectViewID: 1,
					Position:      12,
				},
			},
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		require.Len(t, conn.send, 1)
		payload, ok := (<-conn.send).Data.(projectChangedPayload)
		require.True(t, ok)
		assert.Equal(t, []int64{1}, payload.ProjectIDs)
	})

	t.Run("skips the pseudo view of a saved filter", func(t *testing.T) {
		setupDBTest(t)
		InitHub()
		conn := projectChangeConn(1)
		GetHub().Register(conn)

		// 保存的筛选器视图是负数 ID 的伪视图，project_views 里没有对应行。
		event := &models.TaskPositionsRecalculatedEvent{
			NewTaskPositions: []*models.TaskPosition{
				{
					TaskID:        1,
					ProjectViewID: -15,
					Position:      12,
				},
			},
		}
		events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})

		assert.Empty(t, conn.send)
	})

	t.Run("purging a project reaches the users captured before the delete", func(t *testing.T) {
		s := setupDBTest(t)
		InitHub()
		conn := projectChangeConn(1)
		GetHub().Register(conn)

		// 项目 45 和它的子项目 46 已经在回收站里，清除后行和权限都会消失。
		require.NoError(t, (&models.Project{ID: 45}).DeletePermanently(s, &user.User{ID: 1}))
		require.NoError(t, s.Commit())
		events.ClearDispatchedEvents()
		events.DispatchPending(context.Background(), s)

		purged := events.GetDispatchedEvents((&models.ProjectChangePurgedEvent{}).Name())
		require.Len(t, purged, 2)
		for _, event := range purged {
			events.TestListener(t, event, &ProjectChangeListener{wsEvent: event.Name()})
		}

		require.Len(t, conn.send, 2)
		pushed := [][]int64{}
		for range purged {
			payload, ok := (<-conn.send).Data.(projectChangedPayload)
			require.True(t, ok)
			assert.Equal(t, projectChangeStructure, payload.Kind)
			assert.Equal(t, int64(0), payload.TaskID)
			pushed = append(pushed, payload.ProjectIDs)
		}
		assert.Contains(t, pushed, []int64{45})
	})

	t.Run("returns an error for a payload it cannot decode", func(t *testing.T) {
		setupDBTest(t)
		InitHub()

		listener := &ProjectChangeListener{wsEvent: (&models.TaskUpdatedEvent{}).Name()}
		err := listener.Handle(message.NewMessage("test", []byte("not json")))

		require.Error(t, err)
	})
}

func taskInProject(taskID, projectID int64) *models.Task {
	return &models.Task{
		ID:        taskID,
		ProjectID: projectID,
	}
}

func projectChangeConn(userID int64) *Connection {
	return &Connection{
		userID: userID,
		subscriptions: map[string]bool{
			"project.changed": true,
		},
		send: make(chan OutgoingMessage, 16),
	}
}
