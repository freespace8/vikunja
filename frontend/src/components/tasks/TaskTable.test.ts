import {describe, expect, it, vi} from 'vitest'
import {mount} from '@vue/test-utils'

import type {TaskResponse} from '@/client/queries/tasks'

import Sort from './partials/Sort.vue'
import {ACTIVE_COLUMNS_DEFAULT} from './taskTableColumns'
import TaskTable from './TaskTable.vue'

vi.mock('@/composables/useProjects', () => ({
	useProjects: () => ({
		projects: {
			7: {
				title: 'Alpha',
				views: [
					{id: 70, position: 1, view_kind: 'list'},
					{id: 71, position: 2, view_kind: 'kanban'},
				],
			},
		},
	}),
}))

const task = {
	id: 1,
	project_id: 7,
	title: 'Write tests',
	identifier: '#1',
	index: 1,
	labels: [],
	assignees: [],
	percent_done: 0.5,
} as unknown as TaskResponse

function mountTable(props: Partial<InstanceType<typeof TaskTable>['$props']> = {}) {
	return mount(TaskTable, {
		props: {
			tasks: [task],
			columns: {...ACTIVE_COLUMNS_DEFAULT},
			sortBy: {},
			...props,
		},
		global: {
			mocks: {$t: (key: string) => key},
			stubs: {
				RouterLink: {template: '<a><slot /></a>'},
				Done: true,
				User: true,
				PriorityLabel: true,
				Labels: true,
				TaskGlanceTooltip: {template: '<span><slot /></span>'},
				DateTableCell: {template: '<td />'},
				CommentCount: true,
				AssigneeList: true,
			},
		},
	})
}

function headers(wrapper: ReturnType<typeof mountTable>) {
	return wrapper.findAll('th').map(th => th.text())
}

describe('TaskTable', () => {
	it('renders the default project table columns', () => {
		expect(headers(mountTable())).toEqual([
			'#',
			'task.attributes.done',
			'task.attributes.title',
			'task.attributes.labels',
			'task.attributes.assignees',
			'task.attributes.bucket',
			'task.attributes.dueDate',
		])
	})

	it('shows the bucket of the kanban view of the project, and nothing for a task without one', () => {
		const wrapper = mountTable({
			tasks: [
				{...task, buckets: [{id: 5, title: 'Doing', project_view_id: 71}]} as TaskResponse,
				{...task, id: 2},
			],
			columns: {...ACTIVE_COLUMNS_DEFAULT, title: false, labels: false, assignees: false, dueDate: false},
		})

		expect(wrapper.findAll('tbody tr').map(row => row.findAll('td').map(cell => cell.text())))
			.toEqual([
				['#1', '', 'Doing'],
				['#1', '', ''],
			])
	})

	it('hides the bucket column when it is disabled', () => {
		const wrapper = mountTable({columns: {...ACTIVE_COLUMNS_DEFAULT, bucket: false}})

		expect(headers(wrapper)).not.toContain('task.attributes.bucket')
	})

	it('renders a project column with the project title when it is enabled', () => {
		const wrapper = mountTable({columns: {...ACTIVE_COLUMNS_DEFAULT, project: true}})

		expect(headers(wrapper)).toContain('task.attributes.project')
		expect(wrapper.find('tbody td:nth-child(3)').text()).toBe('Alpha')
	})

	it('renders one cell per enabled column and task', () => {
		const wrapper = mountTable({
			tasks: [task, {...task, id: 2}],
			columns: {...ACTIVE_COLUMNS_DEFAULT, project: true, percentDone: true},
		})

		expect(wrapper.findAll('tbody tr')).toHaveLength(2)
		expect(wrapper.findAll('tbody tr:first-child td')).toHaveLength(9)
		expect(wrapper.find('tbody td:last-child').text()).toBe('50%')
	})

	it('marks the sorted column and emits the new sort', async () => {
		const wrapper = mountTable({sortBy: {title: 'desc'}})

		expect(wrapper.findAll('th').find(th => th.text() === 'task.attributes.title')?.attributes('aria-sort'))
			.toBe('descending')

		const titleSort = wrapper.findAllComponents(Sort)[2]
		await titleSort.trigger('click')

		expect(wrapper.emitted('update:sortBy')).toEqual([[{title: 'asc'}]])
		expect(wrapper.emitted('sorted')).toHaveLength(1)
	})
})
