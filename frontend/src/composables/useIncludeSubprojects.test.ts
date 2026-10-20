import {beforeEach, describe, expect, it, vi} from 'vitest'

const projects = vi.hoisted(() => ({
	state: {isLoading: false, children: [] as {is_archived: boolean}[]},
}))
const auth = vi.hoisted(() => ({state: {isLinkShareAuth: false}}))

vi.mock('@/composables/useProjects', async () => {
	const {reactive} = await import('vue')
	projects.state = reactive(projects.state)
	return {
		useProjects: () => ({
			get isLoading() { return projects.state.isLoading },
			getChildProjects: () => projects.state.children,
		}),
	}
})
vi.mock('@/stores/auth', async () => {
	const {reactive} = await import('vue')
	auth.state = reactive(auth.state)
	return {useAuthStore: () => auth.state}
})

import {shouldIncludeSubprojects, useIncludeSubprojects} from './useIncludeSubprojects'

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

describe('useIncludeSubprojects', () => {
	beforeEach(() => {
		projects.state.isLoading = false
		projects.state.children = [{is_archived: false}]
		auth.state.isLinkShareAuth = false
	})

	it('is unknown while the project list is loading and true once it has a live child', () => {
		projects.state.isLoading = true
		const includeSubprojects = useIncludeSubprojects(() => 1)
		expect(includeSubprojects.value).toBeUndefined()

		projects.state.isLoading = false
		expect(includeSubprojects.value).toBe(true)
	})

	it('is false for a project without live children', () => {
		projects.state.children = [{is_archived: true}]
		expect(useIncludeSubprojects(() => 1).value).toBe(false)
	})

	it('is false for a link share even while the project list is loading', () => {
		projects.state.isLoading = true
		auth.state.isLinkShareAuth = true
		expect(useIncludeSubprojects(() => 1).value).toBe(false)
	})

	it('is false for a pseudo project without waiting for the project list', () => {
		projects.state.isLoading = true
		expect(useIncludeSubprojects(() => -1).value).toBe(false)
	})
})
