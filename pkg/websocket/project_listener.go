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

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"

	"github.com/ThreeDotsLabs/watermill/message"
	"xorm.io/xorm"
)

// projectChangedPayload is deliberately ids-only: clients invalidate what they
// have cached and refetch through the regular API, which applies the current
// permissions and filters. ProjectIDs holds every affected project, ancestors
// included, because a parent's views aggregate its sub-projects' tasks.
type projectChangedPayload struct {
	// ProjectID is the project the event happened in, 0 when the event spans several.
	ProjectID  int64   `json:"project_id"`
	ProjectIDs []int64 `json:"project_ids"`
	// TaskID is 0 for events that are not about a single task.
	TaskID int64 `json:"task_id"`
}

// projectChangeTarget is one project an event touches.
type projectChangeTarget struct {
	projectID int64
	// parentProjectID is set by events that carry the project, letting
	// GetProjectIDsWithAncestors still resolve ancestors once it is gone.
	parentProjectID *int64
}

// ProjectChangeListener pushes one "project.changed" event per user who can read
// an affected project. The event name it handles is read from wsEvent because
// watermill messages carry no event name.
type ProjectChangeListener struct {
	wsEvent string
}

// Name returns the listener name.
func (l *ProjectChangeListener) Name() string { return "websocket.push." + l.wsEvent }

// Handle decodes the changed projects, expands them to their ancestors, and
// pushes the ids to every user with access. Failures only log: a missed refresh
// is corrected by the next fetch, not worth a poison queue entry.
func (l *ProjectChangeListener) Handle(msg *message.Message) error {
	hub := GetHub()
	if hub == nil {
		log.Debugf("WebSocket: hub not initialized, skipping %s push", l.wsEvent)
		return nil
	}

	s := db.NewSession()
	defer s.Close()

	targets, taskID, err := projectChangeTargets(s, l.wsEvent, msg.Payload)
	if err != nil {
		log.Errorf("WebSocket: failed to decode %s for the project change push: %v", l.wsEvent, err)
		return nil
	}
	if len(targets) == 0 {
		return nil
	}

	projectIDs := make([]int64, 0, len(targets))
	seenProjects := make(map[int64]bool, len(targets))
	for _, target := range targets {
		ids, err := models.GetProjectIDsWithAncestors(s, target.projectID, target.parentProjectID)
		if err != nil {
			log.Errorf("WebSocket: failed to resolve the ancestors of project %d: %v", target.projectID, err)
			continue
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
		log.Errorf("WebSocket: failed to resolve the users with access to projects %v: %v", projectIDs, err)
		return nil
	}

	payload := projectChangedPayload{
		ProjectID:  targets[0].projectID,
		ProjectIDs: projectIDs,
		TaskID:     taskID,
	}
	// A batch can span projects; then no single one is "the" project.
	if len(targets) > 1 {
		payload.ProjectID = 0
	}

	for _, userID := range userIDs {
		// The user who made the change gets it too: their other tabs and windows need it as well.
		hub.PublishForUser(userID, "project.changed", payload)
	}

	return nil
}

// projectChangeTargets extracts the projects an event affects. Events that
// carry several tasks return one target per distinct project, so the caller
// notifies each project once no matter how big the batch was.
func projectChangeTargets(s *xorm.Session, event string, payload []byte) (targets []projectChangeTarget, taskID int64, err error) {
	switch event {
	// Every task event carries the task as "task".
	case (&models.TaskCreatedEvent{}).Name(),
		(&models.TaskUpdatedEvent{}).Name(),
		(&models.TaskDeletedEvent{}).Name(),
		(&models.TaskRestoredEvent{}).Name(),
		(&models.TaskPurgedEvent{}).Name(),
		(&models.TaskAssigneeCreatedEvent{}).Name(),
		(&models.TaskAssigneeDeletedEvent{}).Name(),
		(&models.TaskCommentCreatedEvent{}).Name(),
		(&models.TaskCommentUpdatedEvent{}).Name(),
		(&models.TaskCommentDeletedEvent{}).Name(),
		(&models.TaskAttachmentCreatedEvent{}).Name(),
		(&models.TaskAttachmentDeletedEvent{}).Name(),
		(&models.TaskRelationCreatedEvent{}).Name(),
		(&models.TaskRelationDeletedEvent{}).Name():

		var taskEvent struct {
			Task *models.Task `json:"task"`
		}
		if err := json.Unmarshal(payload, &taskEvent); err != nil {
			return nil, 0, err
		}
		if taskEvent.Task == nil || taskEvent.Task.ProjectID <= 0 {
			return nil, 0, nil
		}
		return []projectChangeTarget{{projectID: taskEvent.Task.ProjectID}}, taskEvent.Task.ID, nil

	case (&models.TasksBatchCreatedEvent{}).Name():
		var batch models.TasksBatchCreatedEvent
		if err := json.Unmarshal(payload, &batch); err != nil {
			return nil, 0, err
		}
		seen := make(map[int64]bool, len(batch.Tasks))
		for _, task := range batch.Tasks {
			if task == nil || task.ProjectID <= 0 || seen[task.ProjectID] {
				continue
			}
			seen[task.ProjectID] = true
			targets = append(targets, projectChangeTarget{projectID: task.ProjectID})
		}
		return targets, 0, nil

	case (&models.TaskPositionsRecalculatedEvent{}).Name():
		var recalculated models.TaskPositionsRecalculatedEvent
		if err := json.Unmarshal(payload, &recalculated); err != nil {
			return nil, 0, err
		}
		seen := make(map[int64]bool, len(recalculated.NewTaskPositions))
		for _, position := range recalculated.NewTaskPositions {
			if position == nil || position.ProjectViewID <= 0 {
				continue
			}
			view, err := models.GetProjectViewByID(s, position.ProjectViewID)
			if err != nil {
				return nil, 0, err
			}
			if seen[view.ProjectID] {
				continue
			}
			seen[view.ProjectID] = true
			targets = append(targets, projectChangeTarget{projectID: view.ProjectID})
		}
		return targets, 0, nil

	// Project events carry the project itself, so its ancestors stay resolvable
	// even after it has been trashed or purged.
	case (&models.ProjectCreatedEvent{}).Name(),
		(&models.ProjectUpdatedEvent{}).Name(),
		(&models.ProjectRestoredEvent{}).Name(),
		(&models.ProjectPurgedEvent{}).Name(),
		(&models.ProjectDeletedEvent{}).Name():

		var projectEvent struct {
			Project *models.Project `json:"project"`
		}
		if err := json.Unmarshal(payload, &projectEvent); err != nil {
			return nil, 0, err
		}
		if projectEvent.Project == nil || projectEvent.Project.ID <= 0 {
			return nil, 0, nil
		}
		return []projectChangeTarget{{
			projectID:       projectEvent.Project.ID,
			parentProjectID: projectEvent.Project.ParentProjectID,
		}}, 0, nil
	}

	return nil, 0, nil
}

// registerProjectListeners registers the project.changed push for every event
// that changes what a project view shows.
func registerProjectListeners() {
	for _, event := range projectChangeEventNames() {
		events.RegisterListener(event, &ProjectChangeListener{wsEvent: event})
	}
}

func projectChangeEventNames() []string {
	return []string{
		(&models.TaskCreatedEvent{}).Name(),
		(&models.TaskUpdatedEvent{}).Name(),
		(&models.TaskDeletedEvent{}).Name(),
		(&models.TaskRestoredEvent{}).Name(),
		(&models.TaskPurgedEvent{}).Name(),
		(&models.TasksBatchCreatedEvent{}).Name(),
		(&models.TaskAssigneeCreatedEvent{}).Name(),
		(&models.TaskAssigneeDeletedEvent{}).Name(),
		(&models.TaskCommentCreatedEvent{}).Name(),
		(&models.TaskCommentUpdatedEvent{}).Name(),
		(&models.TaskCommentDeletedEvent{}).Name(),
		(&models.TaskAttachmentCreatedEvent{}).Name(),
		(&models.TaskAttachmentDeletedEvent{}).Name(),
		(&models.TaskRelationCreatedEvent{}).Name(),
		(&models.TaskRelationDeletedEvent{}).Name(),
		(&models.TaskPositionsRecalculatedEvent{}).Name(),
		(&models.ProjectCreatedEvent{}).Name(),
		(&models.ProjectUpdatedEvent{}).Name(),
		(&models.ProjectRestoredEvent{}).Name(),
		(&models.ProjectPurgedEvent{}).Name(),
		(&models.ProjectDeletedEvent{}).Name(),
	}
}
