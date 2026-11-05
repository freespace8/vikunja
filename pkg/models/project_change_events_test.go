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

package models

import (
	"context"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 视图和分桶的改动只通过事件推给其他客户端，所以每个改动路径都要断言真的发了出来。
func TestBucketChangeEvents(t *testing.T) {
	u := &user.User{ID: 1}

	t.Run("create", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		bucket := &Bucket{
			Title:         "new bucket",
			ProjectID:     1,
			ProjectViewID: 4,
		}
		require.NoError(t, bucket.Create(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&BucketCreatedEvent{}).Name())
		require.Len(t, dispatched, 1)
		created := dispatched[0].(*BucketCreatedEvent)
		assert.Equal(t, bucket.ID, created.Bucket.ID)
		assert.Equal(t, int64(1), created.Bucket.ProjectID)
	})

	t.Run("update", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		bucket := &Bucket{
			ID:            1,
			Title:         "renamed",
			ProjectID:     1,
			ProjectViewID: 4,
		}
		require.NoError(t, bucket.Update(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&BucketUpdatedEvent{}).Name())
		require.Len(t, dispatched, 1)
		updated := dispatched[0].(*BucketUpdatedEvent)
		assert.Equal(t, int64(1), updated.Bucket.ID)
		assert.Equal(t, "renamed", updated.Bucket.Title)
	})

	t.Run("delete", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		bucket := &Bucket{
			ID:            2,
			ProjectID:     1,
			ProjectViewID: 4,
		}
		require.NoError(t, bucket.Delete(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&BucketDeletedEvent{}).Name())
		require.Len(t, dispatched, 1)
		assert.Equal(t, int64(2), dispatched[0].(*BucketDeletedEvent).Bucket.ID)
	})

	t.Run("default buckets of a new kanban view are not dispatched", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		// 默认分桶在视图存在前创建，ProjectID 还是 0，没有项目可以通知。
		view := &ProjectView{
			ProjectID: 1,
			Title:     "New Kanban",
			ViewKind:  ProjectViewKindKanban,
		}
		require.NoError(t, view.Create(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		assert.Empty(t, events.GetDispatchedEvents((&BucketCreatedEvent{}).Name()))
		assert.Len(t, events.GetDispatchedEvents((&ProjectViewCreatedEvent{}).Name()), 1)
	})
}

func TestProjectViewChangeEvents(t *testing.T) {
	u := &user.User{ID: 1}

	t.Run("create", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		view := &ProjectView{
			ProjectID: 1,
			Title:     "New List",
			ViewKind:  ProjectViewKindList,
		}
		require.NoError(t, view.Create(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&ProjectViewCreatedEvent{}).Name())
		require.Len(t, dispatched, 1)
		created := dispatched[0].(*ProjectViewCreatedEvent)
		assert.Equal(t, view.ID, created.View.ID)
		assert.Equal(t, int64(1), created.View.ProjectID)
	})

	t.Run("update", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		view := &ProjectView{
			ID:        1,
			ProjectID: 1,
			Title:     "Renamed List",
			ViewKind:  ProjectViewKindList,
		}
		require.NoError(t, view.Update(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&ProjectViewUpdatedEvent{}).Name())
		require.Len(t, dispatched, 1)
		updated := dispatched[0].(*ProjectViewUpdatedEvent)
		assert.Equal(t, int64(1), updated.View.ID)
		assert.Equal(t, "Renamed List", updated.View.Title)
	})

	t.Run("delete", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		events.ClearDispatchedEvents()

		view := &ProjectView{
			ID:        2,
			ProjectID: 1,
		}
		require.NoError(t, view.Delete(s, u))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)

		dispatched := events.GetDispatchedEvents((&ProjectViewDeletedEvent{}).Name())
		require.Len(t, dispatched, 1)
		assert.Equal(t, int64(2), dispatched[0].(*ProjectViewDeletedEvent).View.ID)
	})
}
