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
	"strings"

	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// subprojectIDs returns the non-archived descendants of a project at any depth
// that the user can access, ordered by id.
func subprojectIDs(s *xorm.Session, a web.Auth, projectID int64) (ids []int64, err error) {
	ids = []int64{}
	accessible, err := accessibleProjectIDsCond(s, a, "projects.id")
	if err != nil {
		return nil, err
	}
	err = s.
		Table("projects").
		Select("projects.id").
		Join("INNER", "project_ancestors", "project_ancestors.project_id = projects.id").
		Where(builder.And(
			builder.Eq{"project_ancestors.ancestor_id": projectID},
			builder.Gt{"project_ancestors.depth": 0},
			builder.Eq{"projects.is_archived": false},
			accessible,
		)).
		OrderBy("projects.id").
		Find(&ids)
	if err != nil {
		return nil, fmt.Errorf("could not get sub-projects of project %d: %w", projectID, err)
	}
	return ids, nil
}

// bucketSet is a manual kanban view with its buckets in display order.
type bucketSet struct {
	view    *ProjectView
	buckets []*Bucket
	// defaultID resolves like getDefaultBucketID: the view's default bucket, else the first one.
	defaultID int64
}

func newBucketSet(view *ProjectView, buckets []*Bucket) *bucketSet {
	bs := &bucketSet{
		view:      view,
		buckets:   buckets,
		defaultID: view.DefaultBucketID,
	}
	if bs.defaultID == 0 && len(buckets) > 0 {
		bs.defaultID = buckets[0].ID
	}
	return bs
}

func normalizeBucketTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

// idByTitle returns the first bucket, by position, whose title equals the given one.
func (bs *bucketSet) idByTitle(title string) (int64, bool) {
	title = normalizeBucketTitle(title)
	if title == "" {
		return 0, false
	}
	for _, b := range bs.buckets {
		if normalizeBucketTitle(b.Title) == title {
			return b.ID, true
		}
	}
	return 0, false
}

// parentBucketFor places a bucket of a sub-project's home view on this (the parent's) board:
// by role (done, default), then by title, else the default bucket.
func (bs *bucketSet) parentBucketFor(child *bucketSet, childBucket *Bucket) int64 {
	switch {
	case child.view.DoneBucketID != 0 && childBucket.ID == child.view.DoneBucketID && bs.view.DoneBucketID != 0:
		return bs.view.DoneBucketID
	case childBucket.ID == child.defaultID:
		return bs.defaultID
	}
	if id, has := bs.idByTitle(childBucket.Title); has {
		return id
	}
	return bs.defaultID
}

// childBucketFor is the reverse of parentBucketFor: the bucket of a sub-project's
// home view a card dropped into the given parent bucket moves to.
func (bs *bucketSet) childBucketFor(child *bucketSet, parentBucket *Bucket) (int64, bool) {
	switch {
	case bs.view.DoneBucketID != 0 && parentBucket.ID == bs.view.DoneBucketID && child.view.DoneBucketID != 0:
		return child.view.DoneBucketID, true
	case parentBucket.ID == bs.defaultID && child.defaultID != 0:
		return child.defaultID, true
	}
	return child.idByTitle(parentBucket.Title)
}

// homeKanbanViews returns the home view of each project: its first manual kanban
// view by position, then id. Projects without one are absent from the result.
func homeKanbanViews(s *xorm.Session, projectIDs []int64) (map[int64]*ProjectView, error) {
	views := []*ProjectView{}
	err := s.
		In("project_id", projectIDs).
		Where("view_kind = ? AND bucket_configuration_mode = ?", ProjectViewKindKanban, BucketConfigurationModeManual).
		Find(&views)
	if err != nil {
		return nil, fmt.Errorf("could not get the kanban views of the sub-projects: %w", err)
	}

	// Ordered here, not in SQL: a NULL position sorts differently per database.
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Position != views[j].Position {
			return views[i].Position < views[j].Position
		}
		return views[i].ID < views[j].ID
	})

	homes := make(map[int64]*ProjectView, len(projectIDs))
	for _, v := range views {
		if _, has := homes[v.ProjectID]; !has {
			homes[v.ProjectID] = v
		}
	}
	return homes, nil
}

// subprojectBuckets places the tasks of a parent project's sub-projects in the
// buckets of the parent's manual kanban view, without storing anything: a
// sub-project task sits in the parent bucket its home-view bucket maps to.
type subprojectBuckets struct {
	parentID      int64
	viewID        int64
	descendantIDs []int64

	parent *bucketSet
	// children holds the home view of each sub-project that has one.
	children map[int64]*bucketSet
	// childToParent maps every home-view bucket to the parent bucket it shows in.
	childToParent map[int64]int64
	// nonDefault lists, per parent bucket other than the default one, the child buckets
	// mapped to it. The default bucket takes every other task, so it needs no list.
	nonDefault map[int64][]int64
	// nonDefaultAll is the union of the nonDefault lists.
	nonDefaultAll []int64
}

// newSubprojectBuckets returns nil unless the view is a manual kanban view of a
// real project and projects holds sub-projects of it.
func newSubprojectBuckets(s *xorm.Session, view *ProjectView, projects []*Project) (*subprojectBuckets, error) {
	if view.ProjectID < 1 || view.ViewKind != ProjectViewKindKanban || view.BucketConfigurationMode != BucketConfigurationModeManual {
		return nil, nil
	}

	ids := make([]int64, 0, len(projects))
	for _, p := range projects {
		if p.ID > 0 && p.ID != view.ProjectID {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	return loadSubprojectBuckets(s, view, ids)
}

func loadSubprojectBuckets(s *xorm.Session, view *ProjectView, descendantIDs []int64) (*subprojectBuckets, error) {
	parentBuckets, err := bucketsOfViews(s, []int64{view.ID})
	if err != nil {
		return nil, err
	}
	parent := newBucketSet(view, parentBuckets[view.ID])
	if len(parent.buckets) == 0 {
		return nil, nil
	}

	homes, err := homeKanbanViews(s, descendantIDs)
	if err != nil {
		return nil, err
	}
	viewIDs := make([]int64, 0, len(homes))
	for _, v := range homes {
		viewIDs = append(viewIDs, v.ID)
	}
	childBuckets, err := bucketsOfViews(s, viewIDs)
	if err != nil {
		return nil, err
	}

	sb := &subprojectBuckets{
		parentID:      view.ProjectID,
		viewID:        view.ID,
		descendantIDs: descendantIDs,
		parent:        parent,
		children:      make(map[int64]*bucketSet, len(homes)),
		childToParent: map[int64]int64{},
		nonDefault:    map[int64][]int64{},
	}
	for projectID, home := range homes {
		child := newBucketSet(home, childBuckets[home.ID])
		sb.children[projectID] = child
		for _, b := range child.buckets {
			target := parent.parentBucketFor(child, b)
			sb.childToParent[b.ID] = target
			if target != parent.defaultID {
				sb.nonDefault[target] = append(sb.nonDefault[target], b.ID)
				sb.nonDefaultAll = append(sb.nonDefaultAll, b.ID)
			}
		}
	}
	return sb, nil
}

// bucketsOfViews returns the buckets of the given views by view id, ordered by position, then id.
func bucketsOfViews(s *xorm.Session, viewIDs []int64) (map[int64][]*Bucket, error) {
	byView := make(map[int64][]*Bucket, len(viewIDs))
	if len(viewIDs) == 0 {
		return byView, nil
	}

	buckets := []*Bucket{}
	err := s.
		In("project_view_id", viewIDs).
		OrderBy("position asc, id asc").
		Find(&buckets)
	if err != nil {
		return nil, fmt.Errorf("could not get the buckets of views %v: %w", viewIDs, err)
	}
	for _, b := range buckets {
		byView[b.ProjectViewID] = append(byView[b.ProjectViewID], b)
	}
	return byView, nil
}

// parentBucketFor returns the parent bucket a sub-project's home-view bucket shows in.
func (sb *subprojectBuckets) parentBucketFor(childBucketID int64) int64 {
	if id, has := sb.childToParent[childBucketID]; has {
		return id
	}
	return sb.parent.defaultID
}

func taskIDsInBuckets(bucketCond builder.Cond) *builder.Builder {
	return builder.Select("task_buckets.task_id").From("task_buckets").Where(bucketCond)
}

// cond selects the tasks in a bucket of the parent view: the parent's own tasks
// by their row in the view, and the sub-project tasks by their home-view bucket.
// The default bucket also takes every sub-project task that no other bucket
// claims, which covers tasks without a row in their home view and sub-projects
// without a manual kanban view.
func (sb *subprojectBuckets) cond(parentBucketID int64) builder.Cond {
	conds := []builder.Cond{
		builder.And(
			builder.Eq{"tasks.project_id": sb.parentID},
			builder.In("tasks.id", taskIDsInBuckets(builder.Eq{
				"task_buckets.project_view_id": sb.viewID,
				"task_buckets.bucket_id":       parentBucketID,
			})),
		),
	}

	inDescendants := builder.In("tasks.project_id", sb.descendantIDs)
	if parentBucketID != sb.parent.defaultID {
		if mapped := sb.nonDefault[parentBucketID]; len(mapped) > 0 {
			conds = append(conds, builder.And(inDescendants, builder.In("tasks.id", taskIDsInBuckets(builder.In("task_buckets.bucket_id", mapped)))))
		}
		return builder.Or(conds...)
	}

	if len(sb.nonDefaultAll) == 0 {
		return builder.Or(conds[0], inDescendants)
	}
	return builder.Or(conds[0], builder.And(
		inDescendants,
		builder.NotIn("tasks.id", taskIDsInBuckets(builder.In("task_buckets.bucket_id", sb.nonDefaultAll))),
	))
}

// takeBucketFilter turns a top-level `bucket_id = N` filter, as the load-more
// request of a column sends it, into the aggregate membership condition. The
// atom is removed from the filters because it would otherwise join the parent
// view's task_buckets, which sub-project tasks have no rows in. Any other shape
// keeps its plain meaning.
func (sb *subprojectBuckets) takeBucketFilter(opts *taskSearchOptions) {
	for i, f := range opts.parsedFilters {
		if i > 0 && f.join != filterConcatAnd {
			return
		}
	}

	for i, f := range opts.parsedFilters {
		bucketID, isInt := f.value.(int64)
		if f.field != taskPropertyBucketID || f.comparator != taskFilterComparatorEquals || !isInt {
			continue
		}

		rest := make([]*taskFilter, 0, len(opts.parsedFilters)-1)
		rest = append(rest, opts.parsedFilters[:i]...)
		rest = append(rest, opts.parsedFilters[i+1:]...)
		opts.parsedFilters = rest
		opts.bucketCond = sb.cond(bucketID)
		return
	}
}
