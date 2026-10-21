import {describe, expect, it} from 'vitest'
import {normalizeBucket} from '@/client/queries/kanban'
import {normalizeTask} from '@/client/queries/tasks'
import {sortAggregateBucketTasks, sortAggregateBuckets} from './kanbanSubprojects'

const task = (id: number, project_id: number, position = 0) => normalizeTask({id, project_id, position, title: `T${id}`})
const ids = (tasks: {id: number}[]) => tasks.map(item => item.id)

describe('sortAggregateBucketTasks', () => {
	it('puts the own tasks by position before the sub-project tasks by id', () => {
		const tasks = [task(9, 101), task(2, 100, 300), task(4, 102), task(7, 100, 100), task(3, 101)]
		expect(ids(sortAggregateBucketTasks(tasks, 100))).toEqual([7, 2, 3, 4, 9])
	})
	it('ignores the position of a sub-project task', () => {
		const tasks = [task(5, 101, 10), task(6, 101, 0), task(1, 100, 400)]
		expect(ids(sortAggregateBucketTasks(tasks, 100))).toEqual([1, 5, 6])
	})
	it('breaks a position tie by id, as the server does', () => {
		expect(ids(sortAggregateBucketTasks([task(8, 100, 0), task(2, 100, 0)], 100))).toEqual([2, 8])
	})
	it('does not change the array it is given', () => {
		const tasks = [task(9, 101), task(1, 100)]
		sortAggregateBucketTasks(tasks, 100)
		expect(ids(tasks)).toEqual([9, 1])
	})
	it('sorts every bucket of a board and keeps the other bucket fields', () => {
		const bucket = normalizeBucket({id: 3, title: 'Doing', count: 9, limit: 2, tasks: [task(9, 101), task(1, 100)]})
		const [sorted] = sortAggregateBuckets([bucket], 100)
		expect(ids(sorted.tasks)).toEqual([1, 9])
		expect(sorted).toMatchObject({id: 3, title: 'Doing', count: 9, limit: 2})
	})
})
