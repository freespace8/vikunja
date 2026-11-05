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

// GetUserIDsWithAccessToProjects 返回至少能读其中一个项目的所有用户，已去重。
// 已在查找前被删除的项目不贡献用户，而不是让整次调用失败。
func GetUserIDsWithAccessToProjects(s *xorm.Session, projectIDs []int64) ([]int64, error) {
	_, userIDs, err := getProjectAccessForProjectIDs(s, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("get the users with access to projects %v: %w", projectIDs, err)
	}
	return userIDs, nil
}
