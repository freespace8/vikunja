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

import TaskTable from '@/components/tasks/TaskTable.vue'
import TaskTableColumnPicker from '@/components/tasks/TaskTableColumnPicker.vue'
import ProjectTable from './ProjectTable.vue'

async function mountTable(query: Record<string, string>) {
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [{path: '/', component: {render: () => null}}],
	})
	await router.push({path: '/', query})
	await router.isReady()
	const wrapper = shallowMount(ProjectTable, {
		props: {isLoadingProject: false, projectId: 1, viewId: 10},
		global: {
			plugins: [
				router,
				createPinia(),
				[VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}],
			],
			mocks: {$t: (key: string) => key},
			stubs: {
				ProjectWrapper: {template: '<div><slot name="header"/><slot name="default"/></div>'},
				Card: {template: '<div><slot /></div>'},
			},
		},
	})
	await flushPromises()
	return {router, wrapper}
}

describe('ProjectTable', () => {
	beforeEach(() => {
		localStorage.clear()
		sdk.projectViewTasksList.mockReset()
		sdk.projectViewTasksList.mockResolvedValue({data: {items: [], page: 3, total_pages: 5}})
		includeSubprojects.current = ref<boolean | undefined>(undefined)
	})

	it('keeps the sort and page of the url when the project turns out to have sub-projects', async () => {
		const {router} = await mountTable({sort: 'title:asc', page: '3'})

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

	it('shows the project table columns and no Project column in a plain project', async () => {
		includeSubprojects.current = ref<boolean | undefined>(false)

		const {wrapper} = await mountTable({})

		expect(wrapper.findComponent(TaskTable).props('columns')).toEqual({
			index: true,
			done: true,
			project: false,
			title: true,
			priority: false,
			labels: true,
			assignees: true,
			dueDate: true,
			startDate: false,
			endDate: false,
			percentDone: false,
			created: false,
			updated: false,
			createdBy: false,
			doneAt: false,
			commentCount: false,
			bucket: true,
		})
	})

	it('shows the bucket column for stored columns that predate it and keeps a hidden one hidden', async () => {
		localStorage.setItem('tableViewColumns', JSON.stringify({index: true, labels: false}))
		localStorage.setItem('tableViewColumnsAggregate', JSON.stringify({index: true, bucket: false}))

		includeSubprojects.current = ref<boolean | undefined>(false)
		const plain = await mountTable({})
		expect(plain.wrapper.findComponent(TaskTable).props('columns')).toMatchObject({
			index: true,
			labels: false,
			bucket: true,
		})

		includeSubprojects.current = ref<boolean | undefined>(true)
		const aggregate = await mountTable({})
		expect(aggregate.wrapper.findComponent(TaskTable).props('columns')).toMatchObject({
			index: true,
			bucket: false,
		})
	})

	it('asks for the buckets of the tasks', async () => {
		includeSubprojects.current = ref<boolean | undefined>(false)

		await mountTable({})

		expect(sdk.projectViewTasksList.mock.lastCall?.[0].query.expand).toContain('buckets')
	})

	it('puts the sort chosen in the table into the url', async () => {
		const {router, wrapper} = await mountTable({})
		const table = wrapper.findComponent(TaskTable)

		table.vm.$emit('update:sortBy', {title: 'desc'})
		table.vm.$emit('sorted')
		await flushPromises()

		expect(router.currentRoute.value.query.sort).toBe('title:desc')
	})

	it('stores a column change under the key of the table that is shown', async () => {
		includeSubprojects.current = ref<boolean | undefined>(true)
		const {wrapper} = await mountTable({})
		const picker = wrapper.findComponent(TaskTableColumnPicker)

		picker.vm.$emit('update:modelValue', {...picker.props('modelValue'), priority: true})
		await flushPromises()

		expect(JSON.parse(localStorage.getItem('tableViewColumnsAggregate') ?? '{}')).toMatchObject({priority: true})
		expect(JSON.parse(localStorage.getItem('tableViewColumns') ?? '{}')).toMatchObject({priority: false})
		expect(wrapper.findComponent(TaskTable).props('columns')).toMatchObject({priority: true})
	})
})
