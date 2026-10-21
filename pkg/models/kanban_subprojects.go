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
	"errors"
	"fmt"
	"net/http"
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

// subprojectBucketsFor builds the aggregation for a collection, if it asks for one.
func (tf *TaskCollection) subprojectBucketsFor(s *xorm.Session, view *ProjectView, projects []*Project) (*subprojectBuckets, error) {
	if !tf.includeSubprojects || view == nil {
		return nil, nil
	}
	return newSubprojectBuckets(s, view, projects)
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
// home view a card dropped into the given parent bucket moves to. Candidates come by role, then by title,
// and only one that maps back to the same parent bucket counts, so the card stays where it was dropped.
func (bs *bucketSet) childBucketFor(child *bucketSet, parentBucket *Bucket) (int64, bool) {
	candidates := []int64{}
	if bs.view.DoneBucketID != 0 && parentBucket.ID == bs.view.DoneBucketID && child.view.DoneBucketID != 0 {
		candidates = append(candidates, child.view.DoneBucketID)
	}
	if parentBucket.ID == bs.defaultID && child.defaultID != 0 {
		candidates = append(candidates, child.defaultID)
	}
	if title := normalizeBucketTitle(parentBucket.Title); title != "" {
		for _, b := range child.buckets {
			if normalizeBucketTitle(b.Title) == title {
				candidates = append(candidates, b.ID)
			}
		}
	}
	for _, id := range candidates {
		for _, b := range child.buckets {
			if b.ID == id && bs.parentBucketFor(child, b) == parentBucket.ID {
				return id, true
			}
		}
	}
	return 0, false
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

// takeBucketFilter turns a `bucket_id = N` filter joined by AND all the way up, as the load-more
// request of a column sends it (possibly inside the group the view's own filter wraps it in), into the
// aggregate membership condition. The atom is removed from the filters because it would otherwise join
// the parent view's task_buckets, which sub-project tasks have no rows in. Any other shape keeps its
// plain meaning.
func (sb *subprojectBuckets) takeBucketFilter(opts *taskSearchOptions) {
	rest, bucketID, found := takeBucketAtom(opts.parsedFilters)
	if !found {
		return
	}
	opts.parsedFilters = rest
	opts.bucketCond = sb.cond(bucketID)
}

// takeBucketAtom returns a copy of filters without the first `bucket_id = N` atom it can reach through
// AND-joined filters and groups, dropping groups left empty.
func takeBucketAtom(filters []*taskFilter) (rest []*taskFilter, bucketID int64, found bool) {
	for i, f := range filters {
		if i > 0 && f.join != filterConcatAnd {
			return nil, 0, false
		}
	}

	for i, f := range filters {
		var replacement []*taskFilter
		if group, isGroup := f.value.([]*taskFilter); isGroup {
			if f.includeNulls {
				continue
			}
			inner, id, has := takeBucketAtom(group)
			if !has {
				continue
			}
			bucketID = id
			if len(inner) > 0 {
				c := *f
				c.value = inner
				replacement = []*taskFilter{&c}
			}
		} else {
			id, isInt := f.value.(int64)
			if f.field != taskPropertyBucketID || f.comparator != taskFilterComparatorEquals || !isInt {
				continue
			}
			bucketID = id
		}

		rest = make([]*taskFilter, 0, len(filters))
		rest = append(rest, filters[:i]...)
		rest = append(rest, replacement...)
		rest = append(rest, filters[i+1:]...)
		return rest, bucketID, true
	}
	return nil, 0, false
}

// ErrSubprojectBucketNotFound represents an error where a sub-project task is moved to a parent
// bucket that has no counterpart on the task's own project board.
type ErrSubprojectBucketNotFound struct {
	TaskID      int64
	ProjectID   int64
	BucketTitle string
}

// IsErrSubprojectBucketNotFound checks if an error is ErrSubprojectBucketNotFound.
func IsErrSubprojectBucketNotFound(err error) bool {
	var target ErrSubprojectBucketNotFound
	return errors.As(err, &target)
}

func (err ErrSubprojectBucketNotFound) Error() string {
	return fmt.Sprintf("The task's project has no matching bucket [TaskID: %d, ProjectID: %d, BucketTitle: %s]", err.TaskID, err.ProjectID, err.BucketTitle)
}

// ErrCodeSubprojectBucketNotFound holds the unique world-error code of this error.
// It sits well outside the 10001-10007 run of the upstream kanban errors so that a code added upstream can't collide.
const ErrCodeSubprojectBucketNotFound = 10900

// HTTPError holds the http error description
func (err ErrSubprojectBucketNotFound) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeSubprojectBucketNotFound,
		Message:  "The task's own project has no bucket that matches this one, so the task can't be moved here.",
	}
}

// subprojectMove remembers where a sub-project task was dropped on the parent board.
type subprojectMove struct {
	parentProjectID int64
	parentViewID    int64
	buckets         *subprojectBuckets
}

// redirectToSubproject rewrites a move on the parent's board of a sub-project task into the
// equivalent move on the task's own project board, so the permission checks, the bucket limit and the
// done logic that follow are those of the sub-project. Everything else is left to the regular checks, which
// then deny, or report their own error, as before.
func (b *TaskBucket) redirectToSubproject(s *xorm.Session, a web.Auth) error {
	if !b.allowSubprojectTask {
		return nil
	}
	if _, isShare := a.(*LinkSharing); isShare {
		return nil
	}

	parentBucket := &Bucket{
		ID:            b.BucketID,
		ProjectID:     b.ProjectID,
		ProjectViewID: b.ProjectViewID,
	}
	view, err := parentBucket.getOwnView(s)
	if err != nil {
		return err
	}
	if view.ProjectID < 1 || view.ViewKind != ProjectViewKindKanban || view.BucketConfigurationMode != BucketConfigurationModeManual {
		return nil
	}

	// Before the task is looked up, so a stranger can't probe task ids.
	canRead, _, err := view.CanRead(s, a)
	if err != nil || !canRead {
		return err
	}

	task, err := GetTaskByIDSimple(s, b.TaskID)
	if IsErrTaskDoesNotExist(err) {
		// Left to the regular checks, so a reader gets the same denial for any task id.
		return nil
	}
	if err != nil {
		return err
	}
	if task.ProjectID == view.ProjectID {
		return nil
	}

	descendants, err := subprojectIDs(s, a, view.ProjectID)
	if err != nil {
		return err
	}
	isDescendant := false
	for _, id := range descendants {
		if id == task.ProjectID {
			isDescendant = true
			break
		}
	}
	if !isDescendant {
		return nil
	}

	// getOwnView only returns the view, the bucket itself is needed for its title.
	parentBucket, err = getBucketByID(s, b.BucketID)
	if err != nil {
		return err
	}

	sb, err := loadSubprojectBuckets(s, view, []int64{task.ProjectID})
	if err != nil {
		return err
	}
	var childBucketID int64
	var found bool
	var child *bucketSet
	if sb != nil {
		child = sb.children[task.ProjectID]
	}
	if child != nil {
		childBucketID, found = sb.parent.childBucketFor(child, parentBucket)
	}
	if !found {
		// Only tell users who could move the task on its own board that there is no counterpart.
		canWrite, err := canWriteBucketProject(s, task.ProjectID, a)
		if err != nil || !canWrite {
			return err
		}
		canWrite, err = (&Task{ID: task.ID}).CanWrite(s, a)
		if err != nil || !canWrite {
			return err
		}
		return ErrSubprojectBucketNotFound{
			TaskID:      task.ID,
			ProjectID:   task.ProjectID,
			BucketTitle: parentBucket.Title,
		}
	}

	b.subprojectMove = &subprojectMove{
		parentProjectID: view.ProjectID,
		parentViewID:    view.ID,
		buckets:         sb,
	}
	b.ProjectID = task.ProjectID
	b.ProjectViewID = child.view.ID
	b.BucketID = childBucketID
	return nil
}

// restoreParentBucket gives the response the ids of the parent board, as for one of its own
// tasks: the bucket is the parent bucket the task now shows in, which differs from the drop
// target when the move was rerouted (a repeating task dropped into done).
func (b *TaskBucket) restoreParentBucket(s *xorm.Session) error {
	move := b.subprojectMove
	if move == nil {
		return nil
	}

	b.ProjectID = move.parentProjectID
	b.ProjectViewID = move.parentViewID
	b.BucketID = move.buckets.parentBucketFor(b.BucketID)
	b.subprojectMove = nil

	if b.Bucket == nil {
		return nil
	}
	parentBucket, err := getBucketByID(s, b.BucketID)
	if err != nil {
		return err
	}
	// Left as the sub-project bucket's count: the parent column's total isn't computed here.
	parentBucket.Count = b.Bucket.Count
	b.Bucket = parentBucket
	return nil
}
