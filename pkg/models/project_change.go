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
	"fmt"
	"sort"

	"xorm.io/xorm"
)

// GetProjectIDsWithAncestors 返回项目自身和它的所有祖先，因为父项目的视图会聚合子项目的任务。
// knownParentID 供已知父项目的调用方使用：项目被清除后闭包表里已经没有它的记录，
// 只能靠事件里带的父项目 ID 继续向上解析。传 nil 时完全从数据库解析。
func GetProjectIDsWithAncestors(s *xorm.Session, projectID int64, knownParentID *int64) ([]int64, error) {
	parents, err := GetAllParentProjects(s, projectID)
	if err != nil {
		return nil, fmt.Errorf("get the parents of project %d: %w", projectID, err)
	}

	if len(parents) == 0 && knownParentID != nil && *knownParentID > 0 {
		parents, err = GetAllParentProjects(s, *knownParentID)
		if err != nil {
			return nil, fmt.Errorf("get the parents of project %d: %w", *knownParentID, err)
		}
	}

	ids := make([]int64, 0, len(parents)+1)
	if projectID > 0 {
		ids = append(ids, projectID)
	}
	for id := range parents {
		if id != projectID && id > 0 {
			ids = append(ids, id)
		}
	}
	// map 的遍历顺序不固定，排序让推送的 payload 稳定可测。
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	return ids, nil
}

// readProjectIncludingTrash 读到回收站里的项目：删除事件在事务提交后才处理，那时行已经软删除。
func readProjectIncludingTrash(s *xorm.Session, projectID int64) (*Project, error) {
	if projectID < 1 {
		return nil, ErrProjectDoesNotExist{ID: projectID}
	}
	project, err := getProjectIncludingTrash(s, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectDoesNotExist{ID: projectID}
	}
	return project, nil
}

// GetProjectAccessForProjectIDsIncludingTrash 与 getProjectAccessForProjectIDs 相同，
// 但能解析回收站里的项目，删除事件的监听器靠它找到接收者。
func GetProjectAccessForProjectIDsIncludingTrash(s *xorm.Session, projectIDs []int64) (accessByProject map[int64]map[int64]bool, userIDs []int64, err error) {
	accessByProject, userIDs, err = projectAccessForProjectIDs(s, projectIDs, func(s *xorm.Session, projectID int64) ([]int64, error) {
		return userIDsWithProjectAccess(s, projectID, nil, readProjectIncludingTrash)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("get the users with access to projects %v: %w", projectIDs, err)
	}
	return accessByProject, userIDs, nil
}

// ProjectChangeRecipient 是一个接收推送的用户和该用户能读的项目。
type ProjectChangeRecipient struct {
	UserID     int64   `json:"user_id"`
	ProjectIDs []int64 `json:"project_ids"`
}

// GetProjectChangeRecipients 按用户列出推送时要带的项目。
// 清除后项目和权限行都已不存在，接收者必须在删除前算好，所以事件自己带上这个结果。
func GetProjectChangeRecipients(s *xorm.Session, projectIDs []int64) ([]ProjectChangeRecipient, error) {
	access, userIDs, err := GetProjectAccessForProjectIDsIncludingTrash(s, projectIDs)
	if err != nil {
		return nil, err
	}

	recipients := make([]ProjectChangeRecipient, 0, len(userIDs))
	for _, userID := range userIDs {
		readable := make([]int64, 0, len(projectIDs))
		for _, projectID := range projectIDs {
			if access[projectID][userID] {
				readable = append(readable, projectID)
			}
		}
		if len(readable) == 0 {
			continue
		}
		recipients = append(recipients, ProjectChangeRecipient{
			UserID:     userID,
			ProjectIDs: readable,
		})
	}
	return recipients, nil
}
