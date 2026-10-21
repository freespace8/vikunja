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
	"net/http"
	"strconv"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// kanbanOf returns a project's home view (first manual kanban view by position, then id)
// and its buckets by title.
func kanbanOf(t *testing.T, s *xorm.Session, projectID int64) (*ProjectView, map[string]*Bucket) {
	t.Helper()

	homes, err := homeKanbanViews(s, []int64{projectID})
	require.NoError(t, err)
	view, has := homes[projectID]
	require.True(t, has, "project %d has no manual kanban view", projectID)

	buckets := []*Bucket{}
	require.NoError(t, s.Where("project_view_id = ?", view.ID).Find(&buckets))
	byTitle := make(map[string]*Bucket, len(buckets))
	for _, b := range buckets {
		byTitle[b.Title] = b
	}
	return view, byTitle
}

// placeTask puts a task into a bucket without any of the move side effects.
func placeTask(t *testing.T, s *xorm.Session, taskID int64, bucket *Bucket) {
	t.Helper()

	tb := &TaskBucket{
		TaskID:        taskID,
		ProjectViewID: bucket.ProjectViewID,
		BucketID:      bucket.ID,
	}
	require.NoError(t, tb.upsert(s))
}

func setTaskPosition(t *testing.T, s *xorm.Session, taskID, viewID int64, position float64) {
	t.Helper()

	affected, err := s.
		Where("task_id = ? AND project_view_id = ?", taskID, viewID).
		Cols("position").
		Update(&TaskPosition{Position: position})
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)
}

func newTask(t *testing.T, s *xorm.Session, owner *user.User, projectID int64, title string) *Task {
	t.Helper()

	task := &Task{
		Title:     title,
		ProjectID: projectID,
	}
	require.NoError(t, task.Create(s, owner))
	return task
}

func readBoard(t *testing.T, s *xorm.Session, tc *TaskCollection, a *user.User, page, perPage int) map[string]*Bucket {
	t.Helper()

	result, _, _, err := tc.ReadAll(s, a, "", page, perPage)
	require.NoError(t, err)
	buckets, ok := result.([]*Bucket)
	require.True(t, ok, "expected buckets, got %T", result)

	byTitle := make(map[string]*Bucket, len(buckets))
	for _, b := range buckets {
		byTitle[b.Title] = b
	}
	return byTitle
}

func idStr(id int64) string {
	return strconv.FormatInt(id, 10)
}

func bucketTaskIDs(b *Bucket) []int64 {
	ids := make([]int64, 0, len(b.Tasks))
	for _, task := range b.Tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

type kanbanFixture struct {
	s    *xorm.Session
	tree *subprojectTree

	parentView, childView, grandchildView *ProjectView
	parent, child, grandchild             map[string]*Bucket
}

func setupKanbanFixture(t *testing.T, owner *user.User) *kanbanFixture {
	t.Helper()

	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	t.Cleanup(func() { s.Close() })

	f := &kanbanFixture{
		s:    s,
		tree: createSubprojectTree(t, s, owner),
	}
	require.NoError(t, s.Commit())
	f.parentView, f.parent = kanbanOf(t, s, f.tree.parent.ID)
	f.childView, f.child = kanbanOf(t, s, f.tree.child.ID)
	f.grandchildView, f.grandchild = kanbanOf(t, s, f.tree.grandchild.ID)
	return f
}

func TestKanbanSubprojects_Read(t *testing.T) {
	owner := &user.User{ID: 1, Username: "user1"}

	type fixture = kanbanFixture
	setup := func(t *testing.T) *fixture {
		return setupKanbanFixture(t, owner)
	}

	board := func(f *fixture, aggregate bool) *TaskCollection {
		tc := &TaskCollection{
			ProjectID:     f.tree.parent.ID,
			ProjectViewID: f.parentView.ID,
		}
		if aggregate {
			tc.SetIncludeSubprojects()
		}
		return tc
	}

	taskOf := func(f *fixture, p *Project) int64 {
		return f.tree.tasks[p.ID].ID
	}

	t.Run("without the flag the board holds only the project's own tasks", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])

		buckets := readBoard(t, f.s, board(f, false), owner, 1, 25)

		assert.Equal(t, []int64{taskOf(f, f.tree.parent)}, bucketTaskIDs(buckets["To-Do"]))
		assert.Empty(t, buckets["Doing"].Tasks)
		assert.EqualValues(t, 1, buckets["To-Do"].Count)
	})

	t.Run("a sub-project task shows in the parent bucket with the same title", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])
		placeTask(t, f.s, taskOf(f, f.tree.grandchild), f.grandchild["Doing"])

		buckets := readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.Equal(t, []int64{taskOf(f, f.tree.parent)}, bucketTaskIDs(buckets["To-Do"]))
		assert.ElementsMatch(t, []int64{taskOf(f, f.tree.child), taskOf(f, f.tree.grandchild)}, bucketTaskIDs(buckets["Doing"]))
		assert.Empty(t, buckets["Done"].Tasks)
		assert.NotContains(t, bucketTaskIDs(buckets["To-Do"]), taskOf(f, f.tree.archived))
	})

	t.Run("the title match ignores case and surrounding spaces", func(t *testing.T) {
		f := setup(t)
		f.child["Doing"].Title = "  dOiNg "
		_, err := f.s.ID(f.child["Doing"].ID).Cols("title").Update(f.child["Doing"])
		require.NoError(t, err)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])

		buckets := readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.Equal(t, []int64{taskOf(f, f.tree.child)}, bucketTaskIDs(buckets["Doing"]))
	})

	t.Run("a bucket without a counterpart falls back to the default bucket", func(t *testing.T) {
		f := setup(t)
		review := &Bucket{
			ProjectViewID: f.childView.ID,
			Title:         "Review",
			Position:      250,
		}
		require.NoError(t, review.Create(f.s, owner))
		placeTask(t, f.s, taskOf(f, f.tree.child), review)

		buckets := readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.ElementsMatch(t, []int64{taskOf(f, f.tree.parent), taskOf(f, f.tree.child), taskOf(f, f.tree.grandchild)}, bucketTaskIDs(buckets["To-Do"]))
		assert.Empty(t, buckets["Doing"].Tasks)
	})

	t.Run("done and default buckets map by role even when renamed", func(t *testing.T) {
		f := setup(t)
		for title, renamed := range map[string]string{"Done": "Erledigt", "To-Do": "Inbox"} {
			b := f.child[title]
			b.Title = renamed
			_, err := f.s.ID(b.ID).Cols("title").Update(b)
			require.NoError(t, err)
		}
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Done"])
		// The grandchild task stays in its default bucket, which must land in the parent default.

		buckets := readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.Equal(t, []int64{taskOf(f, f.tree.child)}, bucketTaskIDs(buckets["Done"]))
		assert.ElementsMatch(t, []int64{taskOf(f, f.tree.parent), taskOf(f, f.tree.grandchild)}, bucketTaskIDs(buckets["To-Do"]))
	})

	t.Run("only the first manual kanban view of a sub-project decides", func(t *testing.T) {
		f := setup(t)
		first := &ProjectView{
			ProjectID:               f.tree.child.ID,
			Title:                   "First board",
			ViewKind:                ProjectViewKindKanban,
			BucketConfigurationMode: BucketConfigurationModeManual,
			Position:                1,
		}
		require.NoError(t, first.Create(f.s, owner))
		firstBuckets := []*Bucket{}
		require.NoError(t, f.s.Where("project_view_id = ?", first.ID).Find(&firstBuckets))
		var firstDoing *Bucket
		for _, b := range firstBuckets {
			if b.Title == "Doing" {
				firstDoing = b
			}
		}
		require.NotNil(t, firstDoing)
		childTask := taskOf(f, f.tree.child)
		placeTask(t, f.s, childTask, firstDoing)
		placeTask(t, f.s, childTask, f.child["Done"])

		buckets := readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.Contains(t, bucketTaskIDs(buckets["Doing"]), childTask)
		assert.NotContains(t, bucketTaskIDs(buckets["Done"]), childTask)
	})

	t.Run("a sub-project without a manual kanban view shows in the default bucket", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])
		_, err := f.s.
			Where("project_id = ? AND view_kind = ?", f.tree.grandchild.ID, ProjectViewKindKanban).
			Cols("bucket_configuration_mode").
			Update(&ProjectView{BucketConfigurationMode: BucketConfigurationModeFilter})
		require.NoError(t, err)
		placeTask(t, f.s, taskOf(f, f.tree.grandchild), f.grandchild["Doing"])

		buckets := readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.ElementsMatch(t, []int64{taskOf(f, f.tree.parent), taskOf(f, f.tree.grandchild)}, bucketTaskIDs(buckets["To-Do"]))
		assert.Equal(t, []int64{taskOf(f, f.tree.child)}, bucketTaskIDs(buckets["Doing"]))

		_, err = f.s.Where("project_id = ?", f.tree.grandchild.ID).Delete(&ProjectView{})
		require.NoError(t, err)

		buckets = readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.Contains(t, bucketTaskIDs(buckets["To-Do"]), taskOf(f, f.tree.grandchild))
	})

	t.Run("a task without a row in its home view shows in the default bucket", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])
		_, err := f.s.
			Where("task_id = ? AND project_view_id = ?", taskOf(f, f.tree.child), f.childView.ID).
			Delete(&TaskBucket{})
		require.NoError(t, err)

		buckets := readBoard(t, f.s, board(f, true), owner, 1, 25)

		assert.Contains(t, bucketTaskIDs(buckets["To-Do"]), taskOf(f, f.tree.child))
		assert.Empty(t, buckets["Doing"].Tasks)
	})

	t.Run("count is the column total and pages continue across own and sub-project tasks", func(t *testing.T) {
		f := setup(t)
		childTask := taskOf(f, f.tree.child)
		grandchildTask := taskOf(f, f.tree.grandchild)
		placeTask(t, f.s, childTask, f.child["Doing"])
		placeTask(t, f.s, grandchildTask, f.grandchild["Doing"])
		own := newTask(t, f.s, owner, f.tree.parent.ID, "own doing")
		placeTask(t, f.s, own.ID, f.parent["Doing"])

		var ids []int64
		for page := 1; page <= 3; page++ {
			doing := readBoard(t, f.s, board(f, true), owner, page, 1)["Doing"]
			assert.EqualValues(t, 3, doing.Count)
			ids = append(ids, bucketTaskIDs(doing)...)
		}
		assert.Equal(t, []int64{own.ID, childTask, grandchildTask}, ids)
		assert.Empty(t, readBoard(t, f.s, board(f, true), owner, 4, 1)["Doing"].Tasks)

		toDo := readBoard(t, f.s, board(f, true), owner, 1, 25)["To-Do"]
		assert.EqualValues(t, 1, toDo.Count, "the sub-project tasks are in Doing")
	})

	t.Run("own tasks come first by position, then sub-project tasks by id", func(t *testing.T) {
		f := setup(t)
		ownA := newTask(t, f.s, owner, f.tree.parent.ID, "own a")
		ownB := newTask(t, f.s, owner, f.tree.parent.ID, "own b")
		parentTask := taskOf(f, f.tree.parent)
		setTaskPosition(t, f.s, parentTask, f.parentView.ID, 300)
		setTaskPosition(t, f.s, ownA.ID, f.parentView.ID, 100)
		setTaskPosition(t, f.s, ownB.ID, f.parentView.ID, 250)
		childTask := taskOf(f, f.tree.child)
		grandchildTask := taskOf(f, f.tree.grandchild)
		// The grandchild task is the one with the lower position on its own board.
		setTaskPosition(t, f.s, grandchildTask, f.grandchildView.ID, 1)
		setTaskPosition(t, f.s, childTask, f.childView.ID, 900)

		toDo := readBoard(t, f.s, board(f, true), owner, 1, 25)["To-Do"]

		assert.Equal(t, []int64{ownA.ID, ownB.ID, parentTask, childTask, grandchildTask}, bucketTaskIDs(toDo))
	})

	t.Run("the request filter and the view filter apply to sub-project tasks", func(t *testing.T) {
		f := setup(t)
		_, err := f.s.ID(taskOf(f, f.tree.child)).Cols("done").Update(&Task{Done: true})
		require.NoError(t, err)

		tc := board(f, true)
		tc.Filter = "done = false"
		toDo := readBoard(t, f.s, tc, owner, 1, 25)["To-Do"]
		assert.ElementsMatch(t, []int64{taskOf(f, f.tree.parent), taskOf(f, f.tree.grandchild)}, bucketTaskIDs(toDo))
		assert.EqualValues(t, 2, toDo.Count)

		_, err = f.s.ID(f.parentView.ID).Cols("filter").Update(&ProjectView{
			Filter: &TaskCollection{
				Filter: "done = false",
			},
		})
		require.NoError(t, err)
		toDo = readBoard(t, f.s, board(f, true), owner, 1, 25)["To-Do"]
		assert.ElementsMatch(t, []int64{taskOf(f, f.tree.parent), taskOf(f, f.tree.grandchild)}, bucketTaskIDs(toDo))
	})

	t.Run("the flat bucket_id filter selects the same tasks as the bucket, page by page", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])
		placeTask(t, f.s, taskOf(f, f.tree.grandchild), f.grandchild["Doing"])
		own := newTask(t, f.s, owner, f.tree.parent.ID, "own doing")
		placeTask(t, f.s, own.ID, f.parent["Doing"])
		doneChild := newTask(t, f.s, owner, f.tree.child.ID, "child done task")
		placeTask(t, f.s, doneChild.ID, f.child["Doing"])
		_, err := f.s.ID(doneChild.ID).Cols("done").Update(&Task{Done: true})
		require.NoError(t, err)

		flat := func(filter string, page, perPage int) ([]int64, int64) {
			tc := board(f, true)
			tc.SetForceFlatTasks()
			tc.Filter = filter
			tc.SortBy = []string{"position"}
			result, _, total, err := tc.ReadAll(f.s, owner, "", page, perPage)
			require.NoError(t, err)
			tasks, ok := result.([]*Task)
			require.True(t, ok)
			ids := make([]int64, 0, len(tasks))
			for _, task := range tasks {
				ids = append(ids, task.ID)
			}
			return ids, total
		}

		for _, title := range []string{"To-Do", "Doing", "Done"} {
			bucket := readBoard(t, f.s, board(f, true), owner, 1, 25)[title]
			filter := "bucket_id = " + idStr(bucket.ID)

			paged := []int64{}
			for page := 1; page <= len(bucket.Tasks)+1; page++ {
				ids, total := flat(filter, page, 1)
				assert.Equal(t, bucket.Count, total, title)
				paged = append(paged, ids...)
			}
			assert.Equal(t, bucketTaskIDs(bucket), paged, title)
		}

		doing := readBoard(t, f.s, board(f, true), owner, 1, 25)["Doing"]
		ids, total := flat("(done = false) && bucket_id = "+idStr(doing.ID), 1, 25)
		assert.EqualValues(t, 3, total)
		assert.NotContains(t, ids, doneChild.ID)
		assert.Len(t, ids, 3)

		withoutFlag := board(f, false)
		withoutFlag.SetForceFlatTasks()
		withoutFlag.Filter = "bucket_id = " + idStr(doing.ID)
		result, _, _, err := withoutFlag.ReadAll(f.s, owner, "", 1, 25)
		require.NoError(t, err)
		tasks, ok := result.([]*Task)
		require.True(t, ok)
		require.Len(t, tasks, 1, "without the flag only the own task is in the bucket")
		assert.Equal(t, own.ID, tasks[0].ID)
	})

	t.Run("the flat bucket_id filter finds sub-project tasks when the view has a filter of its own", func(t *testing.T) {
		f := setup(t)
		childTask := taskOf(f, f.tree.child)
		placeTask(t, f.s, childTask, f.child["Doing"])
		_, err := f.s.ID(f.parentView.ID).Cols("filter").Update(&ProjectView{
			Filter: &TaskCollection{
				Filter: "done = false",
			},
		})
		require.NoError(t, err)
		doing := readBoard(t, f.s, board(f, true), owner, 1, 25)["Doing"]
		require.Equal(t, []int64{childTask}, bucketTaskIDs(doing))

		for _, filter := range []string{
			"bucket_id = " + idStr(doing.ID),
			"(priority >= 0) && bucket_id = " + idStr(doing.ID),
		} {
			tc := board(f, true)
			tc.SetForceFlatTasks()
			tc.Filter = filter
			result, _, total, err := tc.ReadAll(f.s, owner, "", 1, 1)
			require.NoError(t, err, filter)
			tasks, ok := result.([]*Task)
			require.True(t, ok)
			require.Len(t, tasks, 1, filter)
			assert.Equal(t, childTask, tasks[0].ID, filter)
			assert.Equal(t, doing.Count, total, filter)
		}
	})

	t.Run("an or-combined bucket filter keeps its plain meaning", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])
		tc := board(f, true)
		tc.SetForceFlatTasks()
		tc.Filter = "done = true || bucket_id = " + idStr(f.parent["Doing"].ID)

		ids := readTaskIDs(t, f.s, tc, owner)

		assert.NotContains(t, ids, taskOf(f, f.tree.child))
	})

	t.Run("filter buckets of the parent view cover sub-project tasks", func(t *testing.T) {
		f := setup(t)
		_, err := f.s.ID(taskOf(f, f.tree.child)).Cols("done").Update(&Task{Done: true})
		require.NoError(t, err)
		view := &ProjectView{
			ProjectID:               f.tree.parent.ID,
			Title:                   "By status",
			ViewKind:                ProjectViewKindKanban,
			BucketConfigurationMode: BucketConfigurationModeFilter,
			BucketConfiguration: []*ProjectViewBucketConfiguration{
				{
					Title:  "Open",
					Filter: &TaskCollection{Filter: "done = false"},
				},
				{
					Title:  "Closed",
					Filter: &TaskCollection{Filter: "done = true"},
				},
			},
		}
		require.NoError(t, view.Create(f.s, owner))

		tc := &TaskCollection{
			ProjectID:     f.tree.parent.ID,
			ProjectViewID: view.ID,
		}
		tc.SetIncludeSubprojects()
		buckets := readBoard(t, f.s, tc, owner, 1, 25)

		assert.ElementsMatch(t, []int64{taskOf(f, f.tree.parent), taskOf(f, f.tree.grandchild)}, bucketTaskIDs(buckets["Open"]))
		assert.Equal(t, []int64{taskOf(f, f.tree.child)}, bucketTaskIDs(buckets["Closed"]))
	})

	t.Run("a user with read access on the parent sees the sub-project tasks in the buckets", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])
		reader := &user.User{ID: 2, Username: "user2"}
		_, err := f.s.Insert(&ProjectUser{
			ProjectID:  f.tree.parent.ID,
			UserID:     reader.ID,
			Permission: PermissionRead,
		})
		require.NoError(t, err)
		require.NoError(t, f.s.Commit())

		buckets := readBoard(t, f.s, board(f, true), reader, 1, 25)

		assert.Equal(t, []int64{taskOf(f, f.tree.child)}, bucketTaskIDs(buckets["Doing"]))
	})

	t.Run("link shares never aggregate", func(t *testing.T) {
		f := setup(t)
		placeTask(t, f.s, taskOf(f, f.tree.child), f.child["Doing"])
		share := &LinkSharing{
			ID:         1,
			ProjectID:  f.tree.parent.ID,
			Permission: PermissionRead,
		}

		result, _, _, err := board(f, true).ReadAll(f.s, share, "", 1, 25)
		require.NoError(t, err)
		buckets, ok := result.([]*Bucket)
		require.True(t, ok)

		var ids []int64
		for _, b := range buckets {
			ids = append(ids, bucketTaskIDs(b)...)
		}
		assert.Equal(t, []int64{taskOf(f, f.tree.parent)}, ids)
	})
}

func TestKanbanSubprojects_Move(t *testing.T) {
	owner := &user.User{ID: 1, Username: "user1"}

	// drop moves a task through the parent's board the way the v2 route does.
	drop := func(f *kanbanFixture, a web.Auth, taskID int64, bucket *Bucket, allow bool) (*TaskBucket, bool, error) {
		tb := &TaskBucket{
			TaskID:        taskID,
			ProjectID:     f.tree.parent.ID,
			ProjectViewID: f.parentView.ID,
			BucketID:      bucket.ID,
		}
		if allow {
			tb.SetAllowSubprojectTask()
		}
		can, err := tb.CanUpdate(f.s, a)
		if err != nil || !can {
			return tb, can, err
		}
		return tb, can, tb.Update(f.s, a)
	}

	bucketOf := func(t *testing.T, f *kanbanFixture, taskID, viewID int64) int64 {
		t.Helper()

		row := &TaskBucket{}
		has, err := f.s.Where("task_id = ? AND project_view_id = ?", taskID, viewID).Get(row)
		require.NoError(t, err)
		require.True(t, has)
		return row.BucketID
	}

	hasRow := func(t *testing.T, f *kanbanFixture, taskID, viewID int64) bool {
		t.Helper()

		has, err := f.s.Where("task_id = ? AND project_view_id = ?", taskID, viewID).Exist(&TaskBucket{})
		require.NoError(t, err)
		return has
	}

	t.Run("a sub-project task moves on its own board and the response carries the parent ids", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID

		tb, can, err := drop(f, owner, childTask, f.parent["Doing"], true)

		require.NoError(t, err)
		require.True(t, can)
		assert.Equal(t, f.child["Doing"].ID, bucketOf(t, f, childTask, f.childView.ID))
		assert.False(t, hasRow(t, f, childTask, f.parentView.ID), "no row is written for the parent view")
		assert.Equal(t, f.tree.parent.ID, tb.ProjectID)
		assert.Equal(t, f.parentView.ID, tb.ProjectViewID)
		assert.Equal(t, f.parent["Doing"].ID, tb.BucketID)
		require.NotNil(t, tb.Bucket)
		assert.Equal(t, f.parent["Doing"].ID, tb.Bucket.ID)
		assert.Equal(t, "Doing", tb.Bucket.Title)
		require.NotNil(t, tb.Task)
		assert.Equal(t, f.tree.child.ID, tb.Task.ProjectID)

		buckets := readBoard(t, f.s, &TaskCollection{
			ProjectID:          f.tree.parent.ID,
			ProjectViewID:      f.parentView.ID,
			includeSubprojects: true,
		}, owner, 1, 25)
		assert.Equal(t, []int64{childTask}, bucketTaskIDs(buckets["Doing"]))
	})

	t.Run("a grandchild task moves through the parent's board too", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		grandchildTask := f.tree.tasks[f.tree.grandchild.ID].ID

		_, _, err := drop(f, owner, grandchildTask, f.parent["Doing"], true)

		require.NoError(t, err)
		assert.Equal(t, f.grandchild["Doing"].ID, bucketOf(t, f, grandchildTask, f.grandchildView.ID))
	})

	t.Run("a move into the parent's done bucket marks the task done on every board of its project", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		other := &ProjectView{
			ProjectID:               f.tree.child.ID,
			Title:                   "Other board",
			ViewKind:                ProjectViewKindKanban,
			BucketConfigurationMode: BucketConfigurationModeManual,
			Position:                1e9,
		}
		require.NoError(t, other.Create(f.s, owner))
		otherDone := &Bucket{}
		has, err := f.s.Where("project_view_id = ? AND title = ?", other.ID, "Done").Get(otherDone)
		require.NoError(t, err)
		require.True(t, has)

		tb, _, err := drop(f, owner, childTask, f.parent["Done"], true)

		require.NoError(t, err)
		assert.Equal(t, f.child["Done"].ID, bucketOf(t, f, childTask, f.childView.ID))
		assert.Equal(t, otherDone.ID, bucketOf(t, f, childTask, other.ID))
		require.NotNil(t, tb.Task)
		assert.True(t, tb.Task.Done)
		assert.Equal(t, f.parent["Done"].ID, tb.BucketID)

		tb, _, err = drop(f, owner, childTask, f.parent["Doing"], true)

		require.NoError(t, err)
		require.NotNil(t, tb.Task)
		assert.False(t, tb.Task.Done)
	})

	t.Run("a repeating task dropped into done goes back to the default bucket and the response says so", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		placeTask(t, f.s, childTask, f.child["Doing"])
		_, err := f.s.ID(childTask).Cols("repeat_after", "due_date").Update(&Task{
			RepeatAfter: 3600,
			DueDate:     time.Now().Add(time.Hour),
		})
		require.NoError(t, err)

		tb, _, err := drop(f, owner, childTask, f.parent["Done"], true)

		require.NoError(t, err)
		assert.Equal(t, f.child["To-Do"].ID, bucketOf(t, f, childTask, f.childView.ID))
		assert.Equal(t, f.parent["To-Do"].ID, tb.BucketID)
		require.NotNil(t, tb.Bucket)
		assert.Equal(t, f.parent["To-Do"].ID, tb.Bucket.ID)
	})

	t.Run("a moved task keeps following the roles when its columns are renamed", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		renamed := f.child["Done"]
		renamed.Title = "Erledigt"
		_, err := f.s.ID(renamed.ID).Cols("title").Update(renamed)
		require.NoError(t, err)

		tb, _, err := drop(f, owner, childTask, f.parent["Done"], true)

		require.NoError(t, err)
		assert.Equal(t, renamed.ID, bucketOf(t, f, childTask, f.childView.ID))
		require.NotNil(t, tb.Task)
		assert.True(t, tb.Task.Done)
	})

	t.Run("a bucket without a counterpart is refused and nothing changes", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		working := f.child["Doing"]
		working.Title = "Working"
		_, err := f.s.ID(working.ID).Cols("title").Update(working)
		require.NoError(t, err)
		before := bucketOf(t, f, childTask, f.childView.ID)

		_, can, err := drop(f, owner, childTask, f.parent["Doing"], true)

		require.Error(t, err)
		assert.False(t, can)
		assert.True(t, IsErrSubprojectBucketNotFound(err))
		assert.Equal(t, ErrCodeSubprojectBucketNotFound, err.(ErrSubprojectBucketNotFound).HTTPError().Code)
		assert.Equal(t, http.StatusBadRequest, err.(ErrSubprojectBucketNotFound).HTTPError().HTTPCode)
		assert.Equal(t, before, bucketOf(t, f, childTask, f.childView.ID))
		assert.False(t, hasRow(t, f, childTask, f.parentView.ID))
	})

	t.Run("a bucket that would show elsewhere on the parent's board is refused", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		// The child's default bucket is called Doing, so its cards show in the parent's default bucket, not in Doing.
		f.childView.DefaultBucketID = f.child["Doing"].ID
		_, err := f.s.ID(f.childView.ID).Cols("default_bucket_id").Update(f.childView)
		require.NoError(t, err)
		placeTask(t, f.s, childTask, f.child["To-Do"])

		_, can, err := drop(f, owner, childTask, f.parent["Doing"], true)

		require.Error(t, err)
		assert.False(t, can)
		assert.True(t, IsErrSubprojectBucketNotFound(err))
		assert.Equal(t, f.child["To-Do"].ID, bucketOf(t, f, childTask, f.childView.ID))
	})

	t.Run("a drop into the parent's default bucket is refused when the child's default bucket is its done bucket", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		// The child's default bucket is also its done bucket, so its cards show in the parent's done bucket.
		f.childView.DefaultBucketID = f.child["Done"].ID
		f.childView.DoneBucketID = f.child["Done"].ID
		_, err := f.s.ID(f.childView.ID).Cols("default_bucket_id", "done_bucket_id").Update(f.childView)
		require.NoError(t, err)
		f.child["To-Do"].Title = "Inbox"
		_, err = f.s.ID(f.child["To-Do"].ID).Cols("title").Update(f.child["To-Do"])
		require.NoError(t, err)
		placeTask(t, f.s, childTask, f.child["Doing"])

		_, can, err := drop(f, owner, childTask, f.parent["To-Do"], true)

		require.Error(t, err)
		assert.False(t, can)
		assert.True(t, IsErrSubprojectBucketNotFound(err))
		assert.Equal(t, f.child["Doing"].ID, bucketOf(t, f, childTask, f.childView.ID))
	})

	t.Run("a sub-project without a manual kanban view is refused", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		_, err := f.s.Where("project_id = ?", f.tree.child.ID).Delete(&ProjectView{})
		require.NoError(t, err)

		_, _, err = drop(f, owner, childTask, f.parent["Doing"], true)

		require.Error(t, err)
		assert.True(t, IsErrSubprojectBucketNotFound(err))
	})

	t.Run("a user who can't write the sub-project gets a denial, not the missing bucket", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		working := f.child["Doing"]
		working.Title = "Working"
		_, err := f.s.ID(working.ID).Cols("title").Update(working)
		require.NoError(t, err)
		reader := &user.User{ID: 2, Username: "user2"}
		_, err = f.s.Insert(&ProjectUser{
			ProjectID:  f.tree.parent.ID,
			UserID:     reader.ID,
			Permission: PermissionRead,
		})
		require.NoError(t, err)

		_, can, err := drop(f, reader, childTask, f.parent["Doing"], true)

		require.NoError(t, err)
		assert.False(t, can)
	})

	t.Run("a reader gets the same denial for a missing task as for an existing one", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		reader := &user.User{ID: 2, Username: "user2"}
		_, err := f.s.Insert(&ProjectUser{
			ProjectID:  f.tree.parent.ID,
			UserID:     reader.ID,
			Permission: PermissionRead,
		})
		require.NoError(t, err)

		_, can, err := drop(f, reader, 999999, f.parent["Doing"], true)

		require.NoError(t, err)
		assert.False(t, can)
	})

	t.Run("the sub-project's bucket limit applies", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		other := newTask(t, f.s, owner, f.tree.child.ID, "already doing")
		placeTask(t, f.s, other.ID, f.child["Doing"])
		_, err := f.s.ID(f.child["Doing"].ID).Cols("limit").Update(&Bucket{Limit: 1})
		require.NoError(t, err)

		_, _, err = drop(f, owner, childTask, f.parent["Doing"], true)

		require.Error(t, err)
		assert.True(t, IsErrBucketLimitExceeded(err))
		assert.Equal(t, f.child["To-Do"].ID, bucketOf(t, f, childTask, f.childView.ID))
	})

	t.Run("the parent's bucket limit does not count sub-project tasks", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		_, err := f.s.ID(f.parent["Doing"].ID).Cols("limit").Update(&Bucket{Limit: 1})
		require.NoError(t, err)
		own := newTask(t, f.s, owner, f.tree.parent.ID, "own doing")
		placeTask(t, f.s, own.ID, f.parent["Doing"])

		_, can, err := drop(f, owner, childTask, f.parent["Doing"], true)

		require.NoError(t, err, "the parent limit only counts the parent's own task rows, which D2 keeps")
		require.True(t, can)
		assert.Equal(t, f.child["Doing"].ID, bucketOf(t, f, childTask, f.childView.ID))
	})

	t.Run("without the flag the move is still forbidden", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID

		_, can, err := drop(f, owner, childTask, f.parent["Doing"], false)

		require.NoError(t, err)
		assert.False(t, can)
	})

	t.Run("an archived sub-project, a stranger and a link share are denied", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		archivedTask := f.tree.tasks[f.tree.archived.ID].ID

		_, can, _ := drop(f, owner, archivedTask, f.parent["Doing"], true)
		assert.False(t, can, "archived sub-project")

		_, can, _ = drop(f, &user.User{ID: 2, Username: "user2"}, childTask, f.parent["Doing"], true)
		assert.False(t, can, "user without access")

		share := &LinkSharing{
			ID:         1,
			ProjectID:  f.tree.parent.ID,
			Permission: PermissionWrite,
		}
		_, can, _ = drop(f, share, childTask, f.parent["Doing"], true)
		assert.False(t, can, "link share")
	})

	t.Run("a task of an unrelated project is denied", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		unrelated := newTask(t, f.s, owner, 1, "elsewhere")

		_, can, _ := drop(f, owner, unrelated.ID, f.parent["Doing"], true)

		assert.False(t, can)
	})

	t.Run("a sub-project task still has no position in the parent view", func(t *testing.T) {
		f := setupKanbanFixture(t, owner)
		childTask := f.tree.tasks[f.tree.child.ID].ID
		tp := &TaskPosition{
			TaskID:        childTask,
			ProjectViewID: f.parentView.ID,
			Position:      10,
		}

		can, err := tp.CanUpdate(f.s, owner)

		require.NoError(t, err)
		assert.False(t, can)
	})
}
