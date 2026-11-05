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

func (b *BucketCreatedEvent) Name() string {
	return "bucket.created"
}

// BucketUpdatedEvent 表示分桶改名、改上限或改了顺序。
type BucketUpdatedEvent struct {
	Bucket    *Bucket    `json:"bucket"`
	ProjectID int64      `json:"project_id"`
	Doer      *user.User `json:"doer"`
}

func (b *BucketUpdatedEvent) Name() string {
	return "bucket.updated"
}

// BucketDeletedEvent 表示分桶被删除，其中的任务本身不删除。
type BucketDeletedEvent struct {
	Bucket    *Bucket    `json:"bucket"`
	ProjectID int64      `json:"project_id"`
	Doer      *user.User `json:"doer"`
}

func (b *BucketDeletedEvent) Name() string {
	return "bucket.deleted"
}

// ProjectViewCreatedEvent 表示项目新增了一个视图。
type ProjectViewCreatedEvent struct {
	View *ProjectView `json:"view"`
	Doer *user.User   `json:"doer"`
}

func (p *ProjectViewCreatedEvent) Name() string {
	return "project.view.created"
}

// ProjectViewUpdatedEvent 表示视图被改名、换类型、改筛选或改分桶配置。
type ProjectViewUpdatedEvent struct {
	View *ProjectView `json:"view"`
	Doer *user.User   `json:"doer"`
}

func (p *ProjectViewUpdatedEvent) Name() string {
	return "project.view.updated"
}

// ProjectViewDeletedEvent 表示视图被删除。
type ProjectViewDeletedEvent struct {
	View *ProjectView `json:"view"`
	Doer *user.User   `json:"doer"`
}

func (p *ProjectViewDeletedEvent) Name() string {
	return "project.view.deleted"
}

// ProjectParentChangedEvent 在项目换了父项目时发出：只带新父项目的项目事件推不到旧父项目的看板。
type ProjectParentChangedEvent struct {
	Project            *Project   `json:"project"`
	OldParentProjectID *int64     `json:"old_parent_project_id"`
	Doer               *user.User `json:"doer"`
}

func (p *ProjectParentChangedEvent) Name() string {
	return "project.parent.changed"
}

// ProjectChangePurgedEvent 在项目被永久清除后发出。
// 项目和权限行都已删除，接收者与项目 ID 必须在删除前算好，所以事件自带这些结果。
type ProjectChangePurgedEvent struct {
	Recipients []ProjectChangeRecipient `json:"recipients"`
}

func (p *ProjectChangePurgedEvent) Name() string {
	return "project.purged.change"
}
