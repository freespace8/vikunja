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

package webtests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kanbanColumn is one bucket of a buckets-with-tasks response.
type kanbanColumn struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Count int64  `json:"count"`
	Tasks []struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		ProjectID int64  `json:"project_id"`
	} `json:"tasks"`
}

func (c kanbanColumn) taskTitles() []string {
	titles := make([]string, 0, len(c.Tasks))
	for _, task := range c.Tasks {
		titles = append(titles, task.Title)
	}
	return titles
}

func (c kanbanColumn) taskID(t *testing.T, title string) int64 {
	t.Helper()
	for _, task := range c.Tasks {
		if task.Title == title {
			return task.ID
		}
	}
	require.FailNow(t, "no task "+title+" in bucket "+c.Title)
	return 0
}

// kanbanProjects creates projects with a single task each through the v2 API.
type kanbanProjects struct {
	t   *testing.T
	e   *echo.Echo
	tok string
}

func (k kanbanProjects) create(title string, parentID int64) (projectID int64) {
	k.t.Helper()
	post := func(path, body string) []byte {
		rec := humaRequest(k.t, k.e, http.MethodPost, path, body, k.tok, "")
		require.Equal(k.t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		return rec.Body.Bytes()
	}
	var created struct {
		ID int64 `json:"id"`
	}
	body := fmt.Sprintf(`{"title":%q,"parent_project_id":%d}`, title, parentID)
	require.NoError(k.t, json.Unmarshal(post("/api/v2/projects", body), &created))
	post(fmt.Sprintf("/api/v2/projects/%d/tasks", created.ID), fmt.Sprintf(`{"title":"%s task"}`, title))
	return created.ID
}

func (k kanbanProjects) kanbanViewID(projectID int64) int64 {
	k.t.Helper()
	rec := humaRequest(k.t, k.e, http.MethodGet, fmt.Sprintf("/api/v2/projects/%d", projectID), "", k.tok, "")
	require.Equal(k.t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var project struct {
		Views []struct {
			ID       int64  `json:"id"`
			ViewKind string `json:"view_kind"`
		} `json:"views"`
	}
	require.NoError(k.t, json.Unmarshal(rec.Body.Bytes(), &project))
	for _, view := range project.Views {
		if view.ViewKind == "kanban" {
			return view.ID
		}
	}
	require.FailNow(k.t, "project has no kanban view")
	return 0
}

// board reads a kanban view's buckets with their tasks, by bucket title.
func (k kanbanProjects) board(projectID, viewID int64, query string) map[string]kanbanColumn {
	k.t.Helper()
	path := fmt.Sprintf("/api/v2/projects/%d/views/%d/buckets/tasks%s", projectID, viewID, query)
	rec := humaRequest(k.t, k.e, http.MethodGet, path, "", k.tok, "")
	require.Equal(k.t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var body struct {
		Items []kanbanColumn `json:"items"`
	}
	require.NoError(k.t, json.Unmarshal(rec.Body.Bytes(), &body))
	columns := make(map[string]kanbanColumn, len(body.Items))
	for _, column := range body.Items {
		columns[column.Title] = column
	}
	return columns
}

func (k kanbanProjects) move(projectID, viewID, bucketID, taskID int64) {
	k.t.Helper()
	rec := humaRequest(k.t, k.e, http.MethodPut,
		fmt.Sprintf("/api/v2/projects/%d/views/%d/buckets/%d/tasks", projectID, viewID, bucketID),
		fmt.Sprintf(`{"task_id":%d}`, taskID), k.tok, "")
	require.Equal(k.t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

// TestHumaKanbanSubprojects covers the parent project's kanban board over the v2 API:
// sub-project tasks shown in the parent's buckets, and moved by dropping them there.
func TestHumaKanbanSubprojects(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	tok := humaTokenFor(t, &testuser1)
	k := kanbanProjects{
		t:   t,
		e:   e,
		tok: tok,
	}

	parentID := k.create("kbparent", 0)
	childID := k.create("kbchild", parentID)
	parentView := k.kanbanViewID(parentID)
	childView := k.kanbanViewID(childID)

	parentBoard := k.board(parentID, parentView, "")
	childBoard := k.board(childID, childView, "")
	childTask := childBoard["To-Do"].taskID(t, "kbchild task")

	t.Run("a task dropped on a parent column moves on the child board", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut,
			fmt.Sprintf("/api/v2/projects/%d/views/%d/buckets/%d/tasks", parentID, parentView, parentBoard["Doing"].ID),
			fmt.Sprintf(`{"task_id":%d}`, childTask), tok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var moved struct {
			BucketID      int64 `json:"bucket_id"`
			ProjectViewID int64 `json:"project_view_id"`
			TaskID        int64 `json:"task_id"`
			Bucket        struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
			} `json:"bucket"`
			Task struct {
				ProjectID int64 `json:"project_id"`
			} `json:"task"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &moved))
		assert.Equal(t, parentBoard["Doing"].ID, moved.BucketID)
		assert.Equal(t, parentView, moved.ProjectViewID)
		assert.Equal(t, childTask, moved.TaskID)
		assert.Equal(t, parentBoard["Doing"].ID, moved.Bucket.ID)
		assert.Equal(t, "Doing", moved.Bucket.Title)
		assert.Equal(t, childID, moved.Task.ProjectID)

		db.AssertExists(t, "task_buckets", map[string]interface{}{
			"task_id":   childTask,
			"bucket_id": childBoard["Doing"].ID,
		}, false)
		db.AssertMissing(t, "task_buckets", map[string]interface{}{
			"task_id":         childTask,
			"project_view_id": parentView,
		})
		assert.Equal(t, []string{"kbchild task"}, k.board(childID, childView, "")["Doing"].taskTitles())
		assert.Equal(t, []string{"kbchild task"}, k.board(parentID, parentView, "?include_subprojects=true")["Doing"].taskTitles())
	})

	t.Run("moving it into done marks it done", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut,
			fmt.Sprintf("/api/v2/projects/%d/views/%d/buckets/%d/tasks", parentID, parentView, parentBoard["Done"].ID),
			fmt.Sprintf(`{"task_id":%d}`, childTask), tok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"done":true`)

		assert.Equal(t, []string{"kbchild task"}, k.board(childID, childView, "")["Done"].taskTitles())
		db.AssertExists(t, "tasks", map[string]interface{}{
			"id":   childTask,
			"done": true,
		}, false)
	})

	t.Run("a column the child board has no counterpart for answers 400 with code 10900", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost,
			fmt.Sprintf("/api/v2/projects/%d/views/%d/buckets", parentID, parentView),
			`{"title":"Only on the parent"}`, tok, "")
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		var created struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

		rec = humaRequest(t, e, http.MethodPut,
			fmt.Sprintf("/api/v2/projects/%d/views/%d/buckets/%d/tasks", parentID, parentView, created.ID),
			fmt.Sprintf(`{"task_id":%d}`, childTask), tok, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), fmt.Sprintf(`"code":%d`, models.ErrCodeSubprojectBucketNotFound))
		db.AssertMissing(t, "task_buckets", map[string]interface{}{
			"task_id":   childTask,
			"bucket_id": created.ID,
		})
	})

	t.Run("v1 still refuses to move a sub-project task through the parent's board", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost,
			fmt.Sprintf("/api/v1/projects/%d/views/%d/buckets/%d/tasks", parentID, parentView, parentBoard["To-Do"].ID),
			fmt.Sprintf(`{"task_id":%d}`, childTask), tok, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("a link share never aggregates", func(t *testing.T) {
		config.ServiceEnableLinkSharing.Set(true)
		insertTestShareWithPermission(t, 61, parentID, models.PermissionRead)
		shareTok, err := auth.NewLinkShareJWTAuthtoken(&models.LinkSharing{
			ID:          61,
			Hash:        "ghsa-qfwc-61",
			ProjectID:   parentID,
			Permission:  models.PermissionRead,
			SharingType: models.SharingTypeWithoutPassword,
			SharedByID:  6,
		})
		require.NoError(t, err)

		rec := humaRequest(t, e, http.MethodGet,
			fmt.Sprintf("/api/v2/projects/%d/views/%d/buckets/tasks?include_subprojects=true", parentID, parentView),
			"", shareTok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "kbparent task")
		assert.NotContains(t, rec.Body.String(), "kbchild task")
	})
}
