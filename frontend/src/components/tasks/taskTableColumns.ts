import {camelCase} from 'change-case'

import type {SortBy} from '@/composables/useTaskList'

export const ACTIVE_COLUMNS_DEFAULT = {
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
}

export type TaskTableColumns = Record<keyof typeof ACTIVE_COLUMNS_DEFAULT, boolean>

export function sortByActiveColumns(sortBy: SortBy, columns: TaskTableColumns): SortBy {
	return (Object.keys(sortBy) as (keyof SortBy)[])
		.filter(prop => columns[camelCase(prop) as keyof TaskTableColumns])
		.reduce<SortBy>((obj, key) => {
			obj[key] = sortBy[key]
			return obj
		}, {})
}
