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
	"xorm.io/xorm"
)

// GetProjectIDsWithAncestors returns projectID together with the ids of all
// projects above it in the hierarchy. Callers notify every one of them because
// a parent's views aggregate the tasks of its sub-projects.
//
// knownParentID short-circuits the first hop for callers that already hold the
// project (project events carry it): a project that was just trashed or purged
// can no longer be loaded, but its parent usually still can. Pass nil to resolve
// the parent from the database. A project that does not exist at all yields only
// the ids resolved so far.
func GetProjectIDsWithAncestors(s *xorm.Session, projectID int64, knownParentID *int64) ([]int64, error) {
	ids := make([]int64, 0, 4)
	visited := map[int64]bool{}
	parent := knownParentID

	for current := projectID; current > 0 && !visited[current]; {
		visited[current] = true
		ids = append(ids, current)

		if parent != nil {
			current = *parent
			// The caller's value describes this project only; further hops come from the database.
			parent = nil
			continue
		}

		project, err := GetProjectSimpleByID(s, current)
		if err != nil {
			if IsErrProjectDoesNotExist(err) {
				break
			}
			return nil, err
		}
		current = project.parentID()
	}

	return ids, nil
}

// GetUserIDsWithAccessToProjects returns the deduplicated ids of every user who
// can read at least one of the given projects. A project removed before the
// lookup runs contributes no users instead of failing the whole call.
func GetUserIDsWithAccessToProjects(s *xorm.Session, projectIDs []int64) ([]int64, error) {
	seen := make(map[int64]bool, len(projectIDs))
	userIDs := make([]int64, 0, len(projectIDs))

	for _, projectID := range projectIDs {
		uids, err := getUserIDsWithProjectAccess(s, projectID)
		if err != nil {
			if IsErrProjectDoesNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, uid := range uids {
			if seen[uid] {
				continue
			}
			seen[uid] = true
			userIDs = append(userIDs, uid)
		}
	}

	return userIDs, nil
}
