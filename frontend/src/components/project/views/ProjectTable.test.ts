import {shallowMount, flushPromises} from '@vue/test-utils'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import {ref} from 'vue'
import {createRouter, createMemoryHistory} from 'vue-router'
import {createPinia} from 'pinia'

const sdk = vi.hoisted(() => ({
	projectViewTasksList: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/stores/auth', () => ({useAuthStore: () => ({settings: {timezone: 'UTC'}})}))
vi.mock('@/message', () => ({error: vi.fn()}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projects: {}})}))
vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

const includeSubprojects = vi.hoisted(() => ({current: undefined as unknown as {value: boolean | undefined}}))
vi.mock('@/composables/useIncludeSubprojects', () => ({
	useIncludeSubprojects: () => includeSubprojects.current,
}))

import ProjectTable from './ProjectTable.vue'

async function mountTable(query: Record<string, string>) {
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [{path: '/', component: {render: () => null}}],
	})
	await router.push({path: '/', query})
	await router.isReady()
	shallowMount(ProjectTable, {
		props: {isLoadingProject: false, projectId: 1, viewId: 10},
		global: {
			plugins: [
				router,
				createPinia(),
				[VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}],
			],
			mocks: {$t: (key: string) => key},
			stubs: {ProjectWrapper: {template: '<div><slot name="default"/></div>'}},
		},
	})
	await flushPromises()
	return router
}

describe('ProjectTable', () => {
	beforeEach(() => {
		localStorage.clear()
		sdk.projectViewTasksList.mockReset()
		sdk.projectViewTasksList.mockResolvedValue({data: {items: [], page: 3, total_pages: 5}})
		includeSubprojects.current = ref<boolean | undefined>(undefined)
	})

	it('keeps the sort and page of the url when the project turns out to have sub-projects', async () => {
		const router = await mountTable({sort: 'title:asc', page: '3'})

		includeSubprojects.current.value = true
		await flushPromises()

		expect(router.currentRoute.value.query).toEqual({sort: 'title:asc', page: '3'})
		expect(sdk.projectViewTasksList).toHaveBeenCalledOnce()
		expect(sdk.projectViewTasksList.mock.lastCall?.[0].query).toMatchObject({
			include_subprojects: true,
			page: 3,
			sort_by: ['title'],
		})
	})

	it('starts the columns of a parent project from the columns the user already chose', async () => {
		localStorage.setItem('tableViewColumns', JSON.stringify({index: true, labels: false, project: false}))
		includeSubprojects.current = ref<boolean | undefined>(true)

		await mountTable({})

		expect(JSON.parse(localStorage.getItem('tableViewColumnsAggregate') ?? '{}'))
			.toMatchObject({index: true, labels: false, project: true})
	})
})
