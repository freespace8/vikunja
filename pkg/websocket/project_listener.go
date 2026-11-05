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
	"encoding/json"
	"fmt"
	"slices"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"

	"github.com/ThreeDotsLabs/watermill/message"
	"xorm.io/xorm"
)

// 变更分类，前端据此决定刷新的力度。
const (
	// projectChangeLayout 可能改变卡片在视图里的位置：增删任务、移动、重新排序。
	projectChangeLayout = "layout"
	// projectChangeContent 只改任务自身的内容，卡片的位置不变。
	projectChangeContent = "content"
	// projectChangeStructure 改的是项目、视图或分桶本身。
	projectChangeStructure = "project"
)

// projectChangedPayload 只带 ID：客户端据此让已有缓存失效，再通过普通 API 重新读取，
// 这样权限和筛选条件都由服务端重新应用。ProjectIDs 包含父项目，因为父项目的视图会聚合子项目的任务。
type projectChangedPayload struct {
	ProjectIDs []int64 `json:"project_ids"`
	// TaskID 只对单个任务的事件非零。
	TaskID int64  `json:"task_id"`
	Kind   string `json:"kind"`
}

// projectChangeTarget 是事件影响到的一个项目。
type projectChangeTarget struct {
	projectID int64
	// parentProjectID 由带上项目本身的事件填入：项目进了回收站或被清除后，
	// 只能靠它继续解析祖先项目。
	parentProjectID *int64
}

// projectChange 是一次推送要处理的内容。
type projectChange struct {
	kind    string
	taskID  int64
	targets []projectChangeTarget
}

// ProjectChangeListener 把变更推给所有能读受影响项目的用户。
// 它处理的事件名存在 wsEvent 里，因为 watermill 的消息不带事件名。
type ProjectChangeListener struct {
	wsEvent string
}

// Name 返回监听器名字。
func (l *ProjectChangeListener) Name() string { return "websocket.push." + l.wsEvent }

// Handle 解析事件、补全祖先项目，然后推给每个有权限的用户。
func (l *ProjectChangeListener) Handle(msg *message.Message) error {
	hub := GetHub()
	if hub == nil {
		log.Warningf("WebSocket: hub not initialized, skipping %s push", l.wsEvent)
		return nil
	}

	s := db.NewSession()
	defer s.Close()

	change, err := projectChangeOf(s, l.wsEvent, msg.Payload)
	if err != nil {
		return fmt.Errorf("decode the %s change: %w", l.wsEvent, err)
	}
	if len(change.targets) == 0 {
		return nil
	}

	projectIDs := make([]int64, 0, len(change.targets))
	seenProjects := make(map[int64]bool, len(change.targets))
	for _, target := range change.targets {
		ids, err := models.GetProjectIDsWithAncestors(s, target.projectID, target.parentProjectID)
		if err != nil {
			return fmt.Errorf("resolve the ancestors of project %d: %w", target.projectID, err)
		}
		for _, id := range ids {
			if seenProjects[id] {
				continue
			}
			seenProjects[id] = true
			projectIDs = append(projectIDs, id)
		}
	}
	if len(projectIDs) == 0 {
		return nil
	}

	userIDs, err := models.GetUserIDsWithAccessToProjects(s, projectIDs)
	if err != nil {
		return fmt.Errorf("resolve the users with access to projects %v: %w", projectIDs, err)
	}

	payload := projectChangedPayload{
		ProjectIDs: projectIDs,
		TaskID:     change.taskID,
		Kind:       change.kind,
	}
	for _, userID := range userIDs {
		// 做改动的用户自己也推：他的其他标签页和窗口同样要刷新。
		hub.PublishForUser(userID, "project.changed", payload)
	}

	return nil
}

// projectChangeOf 把事件解析成受影响的项目。无法识别的事件返回空结果。
func projectChangeOf(s *xorm.Session, event string, payload []byte) (projectChange, error) {
	switch {
	case slices.Contains(contentEventNames(), event):
		return taskChange(payload, projectChangeContent)
	case slices.Contains(layoutTaskEventNames(), event):
		return taskChange(payload, projectChangeLayout)
	case event == (&models.TasksBatchCreatedEvent{}).Name():
		return batchChange(payload)
	case event == (&models.TaskPositionsRecalculatedEvent{}).Name():
		return positionsChange(s, payload)
	}

	return structureChange(event, payload)
}

// taskChange 转成单个任务带来的变更；所有任务事件都把它放在 "task" 字段。
func taskChange(payload []byte, kind string) (projectChange, error) {
	var event struct {
		Task *models.Task `json:"task"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return projectChange{}, err
	}

	change := projectChange{kind: kind}
	if event.Task == nil || event.Task.ProjectID <= 0 {
		return change, nil
	}
	change.taskID = event.Task.ID
	change.targets = []projectChangeTarget{{projectID: event.Task.ProjectID}}
	return change, nil
}

// batchChange 按项目去重，一次批次再大也只通知每个项目一次。
func batchChange(payload []byte) (projectChange, error) {
	change := projectChange{kind: projectChangeLayout}

	var batch models.TasksBatchCreatedEvent
	if err := json.Unmarshal(payload, &batch); err != nil {
		return change, err
	}

	seen := make(map[int64]bool, len(batch.Tasks))
	for _, task := range batch.Tasks {
		if task == nil || task.ProjectID <= 0 || seen[task.ProjectID] {
			continue
		}
		seen[task.ProjectID] = true
		change.targets = append(change.targets, projectChangeTarget{projectID: task.ProjectID})
	}

	return change, nil
}

func positionsChange(s *xorm.Session, payload []byte) (projectChange, error) {
	change := projectChange{kind: projectChangeLayout}

	var recalculated models.TaskPositionsRecalculatedEvent
	if err := json.Unmarshal(payload, &recalculated); err != nil {
		return change, err
	}

	seen := make(map[int64]bool, len(recalculated.NewTaskPositions))
	for _, position := range recalculated.NewTaskPositions {
		// 收藏和保存的筛选器是伪视图，ID 为负且没有数据库行；它们的刷新由任务事件覆盖。
		if position == nil || position.ProjectViewID <= 0 {
			continue
		}
		view, err := models.GetProjectViewByID(s, position.ProjectViewID)
		if err != nil {
			if models.IsErrProjectViewDoesNotExist(err) {
				continue
			}
			return change, err
		}
		if view.ProjectID <= 0 || seen[view.ProjectID] {
			continue
		}
		seen[view.ProjectID] = true
		change.targets = append(change.targets, projectChangeTarget{projectID: view.ProjectID})
	}

	return change, nil
}

// structureChange 解析项目、视图和分桶事件。
func structureChange(event string, payload []byte) (projectChange, error) {
	change := projectChange{kind: projectChangeStructure}

	switch {
	case slices.Contains(projectEventNames(), event):
		var projectEvent struct {
			Project *models.Project `json:"project"`
		}
		if err := json.Unmarshal(payload, &projectEvent); err != nil {
			return change, err
		}
		if projectEvent.Project == nil || projectEvent.Project.ID <= 0 {
			return change, nil
		}
		change.targets = []projectChangeTarget{{
			projectID:       projectEvent.Project.ID,
			parentProjectID: projectEvent.Project.ParentProjectID,
		}}

	case slices.Contains(bucketEventNames(), event):
		// 分桶的 ProjectID 不参与 JSON，事件自己带项目 ID。
		var bucketEvent struct {
			ProjectID int64 `json:"project_id"`
		}
		if err := json.Unmarshal(payload, &bucketEvent); err != nil {
			return change, err
		}
		if bucketEvent.ProjectID <= 0 {
			return change, nil
		}
		change.targets = []projectChangeTarget{{projectID: bucketEvent.ProjectID}}

	case slices.Contains(viewEventNames(), event):
		var viewEvent struct {
			View *models.ProjectView `json:"view"`
		}
		if err := json.Unmarshal(payload, &viewEvent); err != nil {
			return change, err
		}
		if viewEvent.View == nil || viewEvent.View.ProjectID <= 0 {
			return change, nil
		}
		change.targets = []projectChangeTarget{{projectID: viewEvent.View.ProjectID}}
	}

	return change, nil
}

// registerProjectListeners 为每个会改变项目视图内容的事件注册 project.changed 推送。
func registerProjectListeners() {
	for _, event := range projectChangeEventNames() {
		events.RegisterListener(event, &ProjectChangeListener{wsEvent: event})
	}
}

func projectChangeEventNames() []string {
	return slices.Concat(
		contentEventNames(),
		layoutTaskEventNames(),
		projectEventNames(),
		bucketEventNames(),
		viewEventNames(),
		[]string{
			(&models.TasksBatchCreatedEvent{}).Name(),
			(&models.TaskPositionsRecalculatedEvent{}).Name(),
		},
	)
}

// contentEventNames 只改任务内容、不会移动卡片的事件。
func contentEventNames() []string {
	return []string{
		(&models.TaskCommentCreatedEvent{}).Name(),
		(&models.TaskCommentUpdatedEvent{}).Name(),
		(&models.TaskCommentDeletedEvent{}).Name(),
		(&models.TaskAttachmentCreatedEvent{}).Name(),
		(&models.TaskAttachmentDeletedEvent{}).Name(),
		(&models.TaskAssigneeCreatedEvent{}).Name(),
		(&models.TaskAssigneeDeletedEvent{}).Name(),
		(&models.TaskRelationCreatedEvent{}).Name(),
		(&models.TaskRelationDeletedEvent{}).Name(),
	}
}

// layoutTaskEventNames 可能改变卡片所在列或完成状态的任务事件。
func layoutTaskEventNames() []string {
	return []string{
		(&models.TaskCreatedEvent{}).Name(),
		(&models.TaskUpdatedEvent{}).Name(),
		(&models.TaskDeletedEvent{}).Name(),
		(&models.TaskRestoredEvent{}).Name(),
		(&models.TaskPurgedEvent{}).Name(),
	}
}

func projectEventNames() []string {
	return []string{
		(&models.ProjectCreatedEvent{}).Name(),
		(&models.ProjectUpdatedEvent{}).Name(),
		(&models.ProjectRestoredEvent{}).Name(),
		(&models.ProjectDeletedEvent{}).Name(),
		(&models.ProjectPurgedEvent{}).Name(),
	}
}

func bucketEventNames() []string {
	return []string{
		(&models.BucketCreatedEvent{}).Name(),
		(&models.BucketUpdatedEvent{}).Name(),
		(&models.BucketDeletedEvent{}).Name(),
	}
}

func viewEventNames() []string {
	return []string{
		(&models.ProjectViewCreatedEvent{}).Name(),
		(&models.ProjectViewUpdatedEvent{}).Name(),
		(&models.ProjectViewDeletedEvent{}).Name(),
	}
}
