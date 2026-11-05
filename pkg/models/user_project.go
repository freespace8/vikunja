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
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"
	"xorm.io/builder"
	"xorm.io/xorm"
)

// SearchUsersForProject performs the per-project user search shared by both API
// versions: it checks the caller can read the project, then lists the users
// with access to it. canRead is false (with no error) when the caller lacks
// read access, so each handler can map that to its own forbidden response.
func SearchUsersForProject(s *xorm.Session, project *Project, a web.Auth, currentUser *user.User, search string) (users []*user.User, canRead bool, err error) {
	canRead, _, err = project.CanRead(s, a)
	if err != nil {
		return nil, false, err
	}
	if !canRead {
		return nil, false, nil
	}
	users, err = ListUsersFromProject(s, project, currentUser, search)
	if err != nil {
		return nil, true, err
	}
	return users, true, nil
}

// ProjectUIDs hold all kinds of user IDs from accounts who have access to a project
type ProjectUIDs struct {
	ProjectUserID     int64 `xorm:"ulID"`
	TeamProjectUserID int64 `xorm:"tlUID"`
	// Carries the source team so filtering joined rows preserves direct shares.
	TeamProjectTeamID int64 `xorm:"tlTID"`
}

// getUserIDsWithProjectAccess returns the ids of all users who can access the project
// through ownership (of the project or any parent), a direct share or a team share.
func getUserIDsWithProjectAccess(s *xorm.Session, projectID int64) (uids []int64, err error) {
	return getUserIDsWithProjectAccessFiltered(s, projectID, nil)
}

func getUserIDsWithProjectAccessFiltered(s *xorm.Session, projectID int64, teamFilter func(teamID int64) (bool, error)) (uids []int64, err error) {
	return userIDsWithProjectAccess(s, projectID, teamFilter, GetProjectSimpleByID)
}

// readProject 决定怎么读权限链上的项目行：回收站里的项目和清除前的事件也要能解析权限链。
func userIDsWithProjectAccess(s *xorm.Session, projectID int64, teamFilter func(teamID int64) (bool, error), readProject func(*xorm.Session, int64) (*Project, error)) (uids []int64, err error) {
	userids := []*ProjectUIDs{}

	currentProject, err := readProject(s, projectID)
	if err != nil {
		return nil, err
	}

	ownerIDs := []int64{}
	visited := map[int64]bool{}

	for !visited[currentProject.ID] {
		visited[currentProject.ID] = true

		currentUserIDs := []*ProjectUIDs{}
		err = s.
			Select(`ul.user_id as ulID,
			tm2.user_id as tlUID,
			tl.team_id as tlTID`).
			Table("projects").
			Alias("l").
			// User stuff
			Join("LEFT", []string{"users_projects", "ul"}, "ul.project_id = l.id").
			// Team stuff
			Join("LEFT", []string{"team_projects", "tl"}, "l.id = tl.project_id").
			Join("LEFT", []string{"team_members", "tm2"}, "tm2.team_id = tl.team_id").
			// The actual condition
			Where(
				builder.Or(
					builder.Or(builder.Eq{"ul.permission": PermissionRead}),
					builder.Or(builder.Eq{"tl.permission": PermissionRead}),

					builder.Or(builder.Eq{"ul.permission": PermissionWrite}),
					builder.Or(builder.Eq{"tl.permission": PermissionWrite}),

					builder.Or(builder.Eq{"ul.permission": PermissionAdmin}),
					builder.Or(builder.Eq{"tl.permission": PermissionAdmin}),
				),
				builder.Eq{"l.id": currentProject.ID},
			).
			Find(&currentUserIDs)
		if err != nil {
			return nil, err
		}
		userids = append(userids, currentUserIDs...)
		ownerIDs = append(ownerIDs, currentProject.OwnerID)

		if currentProject.parentID() == 0 {
			break
		}

		parent, err := readProject(s, currentProject.parentID())
		if err != nil && !IsErrProjectDoesNotExist(err) {
			return nil, err
		}
		if err != nil && IsErrProjectDoesNotExist(err) {
			break
		}

		currentProject = parent
	}

	// Unmatched LEFT JOIN rows scan as id 0.
	uidmap := make(map[int64]bool)
	addUID := func(id int64) {
		if id > 0 {
			uidmap[id] = true
		}
	}
	for _, id := range ownerIDs {
		addUID(id)
	}
	// Joined rows repeat teams, so cache each team-read check.
	readableTeams := make(map[int64]bool)
	teamIsReadable := func(teamID int64) (bool, error) {
		if teamID <= 0 {
			return false, nil
		}
		if readable, has := readableTeams[teamID]; has {
			return readable, nil
		}
		readable, err := teamFilter(teamID)
		if err != nil {
			return false, err
		}
		readableTeams[teamID] = readable
		return readable, nil
	}
	for _, u := range userids {
		addUID(u.ProjectUserID)
		if teamFilter == nil {
			addUID(u.TeamProjectUserID)
			continue
		}
		readable, err := teamIsReadable(u.TeamProjectTeamID)
		if err != nil {
			return nil, err
		}
		if readable {
			addUID(u.TeamProjectUserID)
		}
	}

	uids = make([]int64, 0, len(uidmap))
	for id := range uidmap {
		uids = append(uids, id)
	}
	return uids, nil
}

func getProjectAccessForTasks(s *xorm.Session, tasks []*Task) (accessByProject map[int64]map[int64]bool, userIDs []int64, err error) {
	projectIDs := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		projectIDs = append(projectIDs, task.ProjectID)
	}
	return getProjectAccessForProjectIDs(s, projectIDs)
}

// getProjectAccessForProjectIDs 返回每个项目能读它的用户，以及所有人去重后的并集。
func getProjectAccessForProjectIDs(s *xorm.Session, projectIDs []int64) (accessByProject map[int64]map[int64]bool, userIDs []int64, err error) {
	return projectAccessForProjectIDs(s, projectIDs, getUserIDsWithProjectAccess)
}

func projectAccessForProjectIDs(s *xorm.Session, projectIDs []int64, getUIDs func(*xorm.Session, int64) ([]int64, error)) (accessByProject map[int64]map[int64]bool, userIDs []int64, err error) {
	accessByProject = map[int64]map[int64]bool{}
	seen := map[int64]bool{}
	for _, projectID := range projectIDs {
		if _, done := accessByProject[projectID]; done {
			continue
		}
		access := map[int64]bool{}
		accessByProject[projectID] = access
		uids, err := getUIDs(s, projectID)
		if err != nil {
			if IsErrProjectDoesNotExist(err) {
				continue
			}
			return nil, nil, err
		}
		for _, uid := range uids {
			access[uid] = true
			if !seen[uid] {
				seen[uid] = true
				userIDs = append(userIDs, uid)
			}
		}
	}
	return accessByProject, userIDs, nil
}

// ListUsersFromProject returns a list with all users who have access to a project, regardless of the method which gave them access
func ListUsersFromProject(s *xorm.Session, l *Project, currentUser *user.User, search string) (users []*user.User, err error) {
	isAdmin, err := (&Project{ID: l.ID}).IsAdmin(s, currentUser)
	if err != nil {
		return nil, err
	}

	var uids []int64
	if isAdmin {
		uids, err = getUserIDsWithProjectAccess(s, l.ID)
	} else {
		uids, err = getUserIDsWithProjectAccessFiltered(s, l.ID, func(teamID int64) (bool, error) {
			t := &Team{ID: teamID}
			canRead, _, err := t.CanRead(s, currentUser)
			if err != nil || canRead || !config.ServiceEnablePublicTeams.GetBool() {
				return canRead, err
			}
			return s.Where(builder.Eq{"id": teamID, "is_public": true}).Exist(&Team{})
		})
	}
	if err != nil {
		return nil, err
	}

	users, err = user.ListUsers(s, search, currentUser, &user.ProjectUserOpts{
		AdditionalCond:              builder.In("id", uids),
		ReturnAllIfNoSearchProvided: true,
		MatchFuzzily:                true,
	})
	return
}
