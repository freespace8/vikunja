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
	"code.vikunja.io/api/pkg/user"
)

// 视图和分桶属于看板结构，前端据此刷新列和列的选择器，所以单独发事件。

// BucketCreatedEvent 表示看板新增了一个分桶。
// 分桶的 ProjectID 不参与 JSON，所以事件自己带上项目 ID。
type BucketCreatedEvent struct {
	Bucket    *Bucket    `json:"bucket"`
	ProjectID int64      `json:"project_id"`
	Doer      *user.User `json:"doer"`
}

// Name defines the name for BucketCreatedEvent
func (b *BucketCreatedEvent) Name() string {
	return "bucket.created"
}

// BucketUpdatedEvent 表示分桶改名、改上限或改了顺序。
type BucketUpdatedEvent struct {
	Bucket    *Bucket    `json:"bucket"`
	ProjectID int64      `json:"project_id"`
	Doer      *user.User `json:"doer"`
}

// Name defines the name for BucketUpdatedEvent
func (b *BucketUpdatedEvent) Name() string {
	return "bucket.updated"
}

// BucketDeletedEvent 表示分桶被删除，其中的任务本身不删除。
type BucketDeletedEvent struct {
	Bucket    *Bucket    `json:"bucket"`
	ProjectID int64      `json:"project_id"`
	Doer      *user.User `json:"doer"`
}

// Name defines the name for BucketDeletedEvent
func (b *BucketDeletedEvent) Name() string {
	return "bucket.deleted"
}

// ProjectViewCreatedEvent 表示项目新增了一个视图。
type ProjectViewCreatedEvent struct {
	View *ProjectView `json:"view"`
	Doer *user.User   `json:"doer"`
}

// Name defines the name for ProjectViewCreatedEvent
func (p *ProjectViewCreatedEvent) Name() string {
	return "project.view.created"
}

// ProjectViewUpdatedEvent 表示视图被改名、换类型、改筛选或改分桶配置。
type ProjectViewUpdatedEvent struct {
	View *ProjectView `json:"view"`
	Doer *user.User   `json:"doer"`
}

// Name defines the name for ProjectViewUpdatedEvent
func (p *ProjectViewUpdatedEvent) Name() string {
	return "project.view.updated"
}

// ProjectViewDeletedEvent 表示视图被删除。
type ProjectViewDeletedEvent struct {
	View *ProjectView `json:"view"`
	Doer *user.User   `json:"doer"`
}

// Name defines the name for ProjectViewDeletedEvent
func (p *ProjectViewDeletedEvent) Name() string {
	return "project.view.deleted"
}
