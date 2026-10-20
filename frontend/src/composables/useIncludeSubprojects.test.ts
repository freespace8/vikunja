import {describe, expect, it} from 'vitest'

import {shouldIncludeSubprojects} from './useIncludeSubprojects'

describe('shouldIncludeSubprojects', () => {
	it('is false for a project without sub-projects', () => {
		expect(shouldIncludeSubprojects(1, [])).toBe(false)
	})

	it('is true when the project has a live sub-project', () => {
		expect(shouldIncludeSubprojects(1, [{is_archived: true}, {is_archived: false}])).toBe(true)
	})

	it('is false when every sub-project is archived', () => {
		expect(shouldIncludeSubprojects(1, [{is_archived: true}])).toBe(false)
	})

	it.each([0, -1, -2])('is false for the pseudo project %i', projectId => {
		expect(shouldIncludeSubprojects(projectId, [{is_archived: false}])).toBe(false)
	})
})
