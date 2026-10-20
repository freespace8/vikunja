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
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// subprojectTree is parent > child > grandchild, plus an archived sibling child
// with its own task, each project holding one task.
type subprojectTree struct {
	parent, child, grandchild, archived *Project
	tasks                               map[int64]*Task
	parentListViewID                    int64
}

func createSubprojectTree(t *testing.T, s *xorm.Session, owner *user.User) *subprojectTree {
	t.Helper()

	tree := &subprojectTree{tasks: map[int64]*Task{}}
	create := func(title string, parentID int64) *Project {
		p := &Project{Title: title}
		if parentID != 0 {
			p.ParentProjectID = &parentID
		}
		require.NoError(t, p.Create(s, owner))
		task := &Task{Title: title + " task", ProjectID: p.ID}
		require.NoError(t, task.Create(s, owner))
		tree.tasks[p.ID] = task
		return p
	}

	tree.parent = create("agg parent", 0)
	tree.child = create("agg child", tree.parent.ID)
	tree.grandchild = create("agg grandchild", tree.child.ID)
	tree.archived = create("agg archived", tree.parent.ID)

	// Archiving materializes is_archived down the tree, so mirror that for a descendant.
	archivedGrandchild := create("agg archived grandchild", tree.archived.ID)
	for _, id := range []int64{tree.archived.ID, archivedGrandchild.ID} {
		_, err := s.ID(id).Cols("is_archived").Update(&Project{IsArchived: true})
		require.NoError(t, err)
	}

	view := &ProjectView{}
	has, err := s.
		Where("project_id = ? AND view_kind = ?", tree.parent.ID, ProjectViewKindList).
		Get(view)
	require.NoError(t, err)
	require.True(t, has)
	tree.parentListViewID = view.ID

	return tree
}

func (tree *subprojectTree) taskIDs(projectIDs ...int64) []int64 {
	ids := make([]int64, 0, len(projectIDs))
	for _, id := range projectIDs {
		ids = append(ids, tree.tasks[id].ID)
	}
	return ids
}

func readTaskIDs(t *testing.T, s *xorm.Session, tc *TaskCollection, a *user.User) []int64 {
	t.Helper()

	result, _, _, err := tc.ReadAll(s, a, "", 1, 50)
	require.NoError(t, err)
	tasks, ok := result.([]*Task)
	require.True(t, ok, "expected a flat task list, got %T", result)

	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func TestTaskCollection_IncludeSubprojects(t *testing.T) {
	owner := &user.User{ID: 1, Username: "user1"}

	setup := func(t *testing.T) (*xorm.Session, *subprojectTree) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		t.Cleanup(func() { s.Close() })
		tree := createSubprojectTree(t, s, owner)
		require.NoError(t, s.Commit())
		return s, tree
	}

	collection := func(tree *subprojectTree, aggregate bool) *TaskCollection {
		tc := &TaskCollection{
			ProjectID:     tree.parent.ID,
			ProjectViewID: tree.parentListViewID,
		}
		tc.SetForceFlatTasks()
		if aggregate {
			tc.SetIncludeSubprojects()
		}
		return tc
	}

	t.Run("returns the project, a child and a grandchild", func(t *testing.T) {
		s, tree := setup(t)

		ids := readTaskIDs(t, s, collection(tree, true), owner)

		assert.ElementsMatch(t, tree.taskIDs(tree.parent.ID, tree.child.ID, tree.grandchild.ID), ids)
	})

	t.Run("without the flag only the project's own tasks come back", func(t *testing.T) {
		s, tree := setup(t)

		ids := readTaskIDs(t, s, collection(tree, false), owner)

		assert.Equal(t, tree.taskIDs(tree.parent.ID), ids)
	})

	t.Run("archived sub-projects and their descendants are left out", func(t *testing.T) {
		s, tree := setup(t)

		ids := readTaskIDs(t, s, collection(tree, true), owner)

		assert.NotContains(t, ids, tree.tasks[tree.archived.ID].ID)
		assert.Len(t, ids, 3)
	})

	t.Run("a leaf project behaves as without the flag", func(t *testing.T) {
		s, tree := setup(t)
		tc := &TaskCollection{ProjectID: tree.grandchild.ID}
		tc.SetForceFlatTasks()
		tc.SetIncludeSubprojects()

		ids := readTaskIDs(t, s, tc, owner)

		assert.Equal(t, tree.taskIDs(tree.grandchild.ID), ids)
	})

	t.Run("the request filter applies to the sub-project tasks too", func(t *testing.T) {
		s, tree := setup(t)
		done := tree.tasks[tree.child.ID]
		_, err := s.ID(done.ID).Cols("done").Update(&Task{Done: true})
		require.NoError(t, err)
		tc := collection(tree, true)
		tc.Filter = "done = false"

		ids := readTaskIDs(t, s, tc, owner)

		assert.ElementsMatch(t, tree.taskIDs(tree.parent.ID, tree.grandchild.ID), ids)
	})

	t.Run("sorting by position does not fail for tasks without a position in the view", func(t *testing.T) {
		s, tree := setup(t)
		tc := collection(tree, true)
		tc.SortBy = []string{"position", "id"}
		tc.OrderBy = []string{"asc", "desc"}

		ids := readTaskIDs(t, s, tc, owner)

		assert.ElementsMatch(t, tree.taskIDs(tree.parent.ID, tree.child.ID, tree.grandchild.ID), ids)
	})

	t.Run("a user with read access on the parent sees the sub-project tasks", func(t *testing.T) {
		s, tree := setup(t)
		reader := &user.User{ID: 2, Username: "user2"}
		_, err := s.Insert(&ProjectUser{
			ProjectID:  tree.parent.ID,
			UserID:     reader.ID,
			Permission: PermissionRead,
		})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		ids := readTaskIDs(t, s, collection(tree, true), reader)

		assert.ElementsMatch(t, tree.taskIDs(tree.parent.ID, tree.child.ID, tree.grandchild.ID), ids)
	})

	t.Run("a user without access to the project is still denied", func(t *testing.T) {
		s, tree := setup(t)
		stranger := &user.User{ID: 2, Username: "user2"}

		_, _, _, err := collection(tree, true).ReadAll(s, stranger, "", 1, 50)

		require.Error(t, err)
		assert.True(t, IsErrUserDoesNotHaveAccessToProject(err))
	})

	t.Run("link shares stay on the shared project", func(t *testing.T) {
		s, tree := setup(t)
		share := &LinkSharing{
			ID:         1,
			ProjectID:  tree.parent.ID,
			Permission: PermissionRead,
		}

		result, _, _, err := collection(tree, true).ReadAll(s, share, "", 1, 50)
		require.NoError(t, err)

		tasks, ok := result.([]*Task)
		require.True(t, ok)
		require.Len(t, tasks, 1)
		assert.Equal(t, tree.tasks[tree.parent.ID].ID, tasks[0].ID)
	})
}
