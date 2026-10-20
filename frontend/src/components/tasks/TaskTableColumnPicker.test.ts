import {describe, expect, it} from 'vitest'
import {mount} from '@vue/test-utils'

import {ACTIVE_COLUMNS_DEFAULT} from './taskTableColumns'
import TaskTableColumnPicker from './TaskTableColumnPicker.vue'

function mountPicker(modelValue = {...ACTIVE_COLUMNS_DEFAULT}) {
	return mount(TaskTableColumnPicker, {
		props: {modelValue},
		global: {
			mocks: {$t: (key: string) => key},
			stubs: {
				XButton: true,
				Card: {template: '<div><slot /></div>'},
				Popup: {template: '<div><slot name="content" :is-open="true" /></div>'},
			},
		},
	})
}

describe('TaskTableColumnPicker', () => {
	it('lists every column in the order of the project table', () => {
		const labels = mountPicker().findAll('.fancy-checkbox__content').map(checkbox => checkbox.text())

		expect(labels).toEqual([
			'#',
			'task.attributes.done',
			'task.attributes.project',
			'task.attributes.title',
			'task.attributes.priority',
			'task.attributes.labels',
			'task.attributes.assignees',
			'task.attributes.commentCount',
			'task.attributes.dueDate',
			'task.attributes.startDate',
			'task.attributes.endDate',
			'task.attributes.percentDone',
			'task.attributes.doneAt',
			'task.attributes.created',
			'task.attributes.updated',
			'task.attributes.createdBy',
		])
	})

	it('emits a new columns object instead of changing the one it was given', async () => {
		const columns = {...ACTIVE_COLUMNS_DEFAULT}
		const wrapper = mountPicker(columns)

		await wrapper.findAll('input[type="checkbox"]')[2].setValue(true)

		expect(wrapper.emitted('update:modelValue')).toEqual([[{...ACTIVE_COLUMNS_DEFAULT, project: true}]])
		expect(columns.project).toBe(false)
	})
})
