import {describe, expect, it} from 'vitest'

import {ACTIVE_COLUMNS_DEFAULT, sortByActiveColumns} from './taskTableColumns'

describe('sortByActiveColumns', () => {
	it('drops the sort of hidden columns and keeps the order of the others', () => {
		const sortBy = sortByActiveColumns(
			{priority: 'asc', due_date: 'desc', title: 'asc'},
			{...ACTIVE_COLUMNS_DEFAULT, priority: false},
		)

		expect(Object.entries(sortBy)).toEqual([
			['due_date', 'desc'],
			['title', 'asc'],
		])
	})
})
