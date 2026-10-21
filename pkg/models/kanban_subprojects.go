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
