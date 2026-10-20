import {createPinia} from 'pinia'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import {reactive} from 'vue'
import {enableAutoUnmount, flushPromises, shallowMount} from '@vue/test-utils'
import {createMemoryHistory, createRouter} from 'vue-router'

const sdk = vi.hoisted(() => ({
	tasksList: vi.fn(async (_request: {query: Record<string, unknown>}) => ({
		data: {items: [] as unknown[], total_pages: 1},
	})),
}))

vi.mock('@/client/generated', () => sdk)

const auth = reactive({
	authenticated: true,
	settings: {frontend_settings: {filter_id_used_on_overview: undefined, sidebar_width: 250}},
})

vi.mock('@/stores/auth', () => ({useAuthStore: () => auth}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projects: {}})}))
vi.mock('@/composables/useLabels', () => ({useLabels: () => ({getLabelById: vi.fn()})}))
vi.mock('@/helpers/setTitle', () => ({setTitle: vi.fn()}))
vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

import TaskTable from '@/components/tasks/TaskTable.vue'
import TaskTableColumnPicker from '@/components/tasks/TaskTableColumnPicker.vue'
import ShowTasks from './ShowTasks.vue'

enableAutoUnmount(afterEach)

async function mountUpcoming() {
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [{path: '/', component: {render: () => null}}],
	})
	await router.push('/')
	return shallowMount(ShowTasks, {
		props: {
			dateFrom: 'now/d',
			dateTo: 'now/d+1d',
		},
		global: {
			plugins: [
				createPinia(),
				router,
				[VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {staleTime: 0, retry: false}}})}],
			],
			mocks: {$t: (key: string) => key},
			stubs: {
				Card: {template: '<div><slot /></div>'},
				XButton: {template: '<button><slot /></button>'},
				'i18n-t': true,
				DatepickerWithRange: {render: () => null},
			},
			directives: {tooltip: () => {}, cy: () => {}},
		},
	})
}

function lastQuery(): Record<string, unknown> {
	return sdk.tasksList.mock.lastCall![0].query
}

describe('Upcoming filters', () => {
	beforeEach(() => {
		localStorage.clear()
		sdk.tasksList.mockClear()
	})

	it('reloads tasks when overdue tasks are enabled and disabled', async () => {
		const wrapper = await mountUpcoming()
		await flushPromises()
		expect(sdk.tasksList).toHaveBeenCalledTimes(1)
		expect(lastQuery().filter).toBe("done = false && due_date < 'now/d+1d' && due_date > 'now/d'")

		await wrapper.setProps({showOverdue: true})
		await flushPromises()
		expect(sdk.tasksList).toHaveBeenCalledTimes(2)
		expect(lastQuery().filter).toBe("done = false && due_date < 'now/d+1d'")

		await wrapper.setProps({showOverdue: false})
		await flushPromises()
		expect(sdk.tasksList).toHaveBeenCalledTimes(3)
		expect(lastQuery().filter).toBe("done = false && due_date < 'now/d+1d' && due_date > 'now/d'")
	})

	it('reloads tasks when undated tasks are enabled and disabled', async () => {
		const wrapper = await mountUpcoming()
		await flushPromises()

		await wrapper.setProps({showNulls: true})
		await flushPromises()
		expect(sdk.tasksList).toHaveBeenCalledTimes(2)
		expect(lastQuery().filter_include_nulls).toBe(true)

		await wrapper.setProps({showNulls: false})
		await flushPromises()
		expect(sdk.tasksList).toHaveBeenCalledTimes(3)
		expect(lastQuery().filter_include_nulls).toBe(false)
	})

	it('does not reload tasks when sidebar settings change', async () => {
		await mountUpcoming()
		await flushPromises()

		auth.settings = {frontend_settings: {filter_id_used_on_overview: undefined, sidebar_width: 300}}
		await flushPromises()
		expect(sdk.tasksList).toHaveBeenCalledTimes(1)
	})
})

describe('Overview view mode', () => {
	const task = {
		id: 1,
		project_id: 1,
		title: 'A task',
		labels: [],
		assignees: [],
	}

	beforeEach(() => {
		localStorage.clear()
		sdk.tasksList.mockReset()
		sdk.tasksList.mockResolvedValue({data: {items: [task], total_pages: 1}})
	})

	async function showTable() {
		localStorage.setItem('overviewViewMode', 'table')
		const wrapper = await mountUpcoming()
		await flushPromises()
		return wrapper
	}

	it('shows the list by default', async () => {
		const wrapper = await mountUpcoming()
		await flushPromises()

		expect(wrapper.findComponent(TaskTable).exists()).toBe(false)
		expect(wrapper.find('.tasks').exists()).toBe(true)
		expect(lastQuery().sort_by).toEqual(['due_date', 'id'])
		expect(lastQuery().order_by).toEqual(['asc', 'desc'])
	})

	it('switches to the table, remembers it and keeps the filters', async () => {
		const wrapper = await mountUpcoming()
		await flushPromises()
		const filter = lastQuery().filter

		await wrapper.findAll('.view-mode-switch button')[1].trigger('click')
		await flushPromises()

		expect(localStorage.getItem('overviewViewMode')).toBe('table')
		expect(wrapper.findComponent(TaskTable).exists()).toBe(true)
		expect(wrapper.find('.tasks').exists()).toBe(false)
		expect(sdk.tasksList).toHaveBeenCalledTimes(1)
		expect(lastQuery().filter).toBe(filter)
	})

	it('switches back to the list', async () => {
		const wrapper = await showTable()

		await wrapper.findAll('.view-mode-switch button')[0].trigger('click')

		expect(localStorage.getItem('overviewViewMode')).toBe('list')
		expect(wrapper.findComponent(TaskTable).exists()).toBe(false)
		expect(wrapper.find('.tasks').exists()).toBe(true)
	})

	it('shows the Project column of the table by default', async () => {
		const wrapper = await showTable()

		expect(wrapper.findComponent(TaskTable).props('columns')).toMatchObject({project: true, title: true})
		expect(wrapper.findComponent(TaskTableColumnPicker).exists()).toBe(true)
	})

	it('remembers the column choice under its own key', async () => {
		const wrapper = await showTable()
		const picker = wrapper.findComponent(TaskTableColumnPicker)

		picker.vm.$emit('update:modelValue', {...picker.props('modelValue'), project: false})
		await flushPromises()

		expect(JSON.parse(localStorage.getItem('overviewTableColumns') ?? '{}')).toMatchObject({project: false})
		expect(localStorage.getItem('tableViewColumns')).toBeNull()
		expect(wrapper.findComponent(TaskTable).props('columns')).toMatchObject({project: false})
	})

	it('sorts by the column that was clicked and keeps the filters', async () => {
		const wrapper = await showTable()
		const filter = lastQuery().filter

		wrapper.findComponent(TaskTable).vm.$emit('update:sortBy', {title: 'desc'})
		await flushPromises()

		expect(sdk.tasksList).toHaveBeenCalledTimes(2)
		expect(lastQuery().sort_by).toEqual(['title', 'id'])
		expect(lastQuery().order_by).toEqual(['desc', 'desc'])
		expect(lastQuery().filter).toBe(filter)
	})

	it('ignores the sort of a column that is hidden', async () => {
		localStorage.setItem('overviewTableSortBy', JSON.stringify({priority: 'asc'}))

		await showTable()

		expect(lastQuery().sort_by).toEqual(['due_date', 'id'])
	})
})
