import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient, QueryObserver} from '@tanstack/vue-query'
import {taskKeys, tasksQuery} from './tasks'
import {
	bucketKeys,
	kanbanKeys,
	kanbanQuery,
	normalizeBucket,
	type BoardData,
} from './kanban'
import {
	addTaskAssigneeMutationOptions,
	bulkCreateTasksMutationOptions,
	createTaskMutationOptions,
	deleteTaskMutationOptions,
	duplicateTaskMutationOptions,
	moveTaskMutationOptions,
	taskWriteBody,
	updateTaskMutationOptions,
	updateTaskPositionMutationOptions,
} from './taskMutations'
import type {Task, PaginatedTask} from '@/client/generated'

const sdk = vi.hoisted(() => ({
	bucketsList: vi.fn(),
	patchTasksRead: vi.fn(),
	projectTasksList: vi.fn(),
	projectViewTasksList: vi.fn(),
	projectViewBucketsTasksList: vi.fn(),
	taskAssigneesCreate: vi.fn(),
	taskBucketUpdate: vi.fn(),
	tasksBulkCreate: vi.fn(),
	tasksCreate: vi.fn(),
	tasksDelete: vi.fn(),
	tasksDuplicate: vi.fn(),
	tasksPositionUpdate: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	translatedError: (message: string) => new Error(message),
}))
vi.mock('@/client/requestContext', () => ({
	captureClientRequestContext: () => 1,
	assertClientRequestContext: vi.fn(),
	isClientRequestContextCurrent: vi.fn(() => true),
	isRequestContextAbort: () => false,
}))

async function seedScrolledBoard() {
	const {projectKeys} = await import('./projects')
	const client = new QueryClient()
	sdk.projectViewBucketsTasksList.mockResolvedValue({
		data: {
			items: [{
				id: 3,
				count: 30,
				tasks: Array.from({length: 30}, (_, index) => ({id: index + 1, project_id: 1})),
			}],
		},
	})
	sdk.projectTasksList.mockResolvedValue({data: {items: [], total: 0}})
	client.setQueryData(projectKeys.detail(1), {
		id: 1,
		views: [{id: 2, bucket_configuration_mode: 'manual', default_bucket_id: 3}],
	})
	const board = kanbanQuery(1, 2)
	const list = tasksQuery({project: 1})
	const observers = [
		new QueryObserver(client, {queryKey: board.queryKey, queryFn: board.queryFn}),
		new QueryObserver(client, {queryKey: list.queryKey, queryFn: list.queryFn}),
	].map(observer => observer.subscribe(() => {}))
	await vi.waitFor(() => {
		expect(client.getQueryData(board.queryKey)).toBeDefined()
		expect(client.getQueryData(list.queryKey)).toBeDefined()
	})
	client.setQueryData<BoardData>(board.queryKey, current => current && {...current, pages: {3: 2}})
	return {client, board, list, unsubscribe: () => observers.forEach(stop => stop())}
}

async function seedDefaultBucketBoards() {
	const {projectKeys} = await import('./projects')
	const client = new QueryClient()
	client.setQueryData(projectKeys.detail(1), {
		id: 1,
		views: [
			{id: 2, bucket_configuration_mode: 'manual', default_bucket_id: 4},
			{id: 5, bucket_configuration_mode: 'manual'},
		],
	})
	client.setQueryData(kanbanKeys.board(1, 2), {
		buckets: [
			normalizeBucket({id: 3, count: 0, tasks: []}),
			normalizeBucket({id: 4, count: 0, tasks: []}),
		],
		pages: {3: 1, 4: 1},
	})
	client.setQueryData(kanbanKeys.board(1, 5), {
		buckets: [normalizeBucket({id: 6, count: 0, tasks: []})],
		pages: {6: 1},
	})
	return client
}

describe('task mutations', () => {
	beforeEach(() => vi.resetAllMocks())

	it('sends only writable fields and reconciles loaded detail', async () => {
		const client = new QueryClient()
		client.setQueryData(taskKeys.detail(1), {id: 1, title: 'old'})
		sdk.patchTasksRead.mockResolvedValue({data: {id: 1, title: 'new'}})
		await client.getMutationCache()
			.build(client, updateTaskMutationOptions())
			.execute({id: 1, title: 'new', labels: [{id: 2}], created: '2026-01-01'})
		expect(sdk.patchTasksRead).toHaveBeenCalledWith({
			path: {task: 1},
			body: [{op: 'add', path: '/title', value: 'new'}],
		})
		expect(client.getQueryData<Task>(taskKeys.detail(1))?.title).toBe('new')
	})

	describe('with an open list that includes sub-project tasks', () => {
		async function openAggregateList() {
			const client = new QueryClient()
			const scope = {project: 100, view: 10, params: {include_subprojects: true}}
			sdk.projectViewTasksList.mockResolvedValue({
				data: {items: [{id: 3, project_id: 101, title: 'child task'}], total: 1},
			})
			const {queryKey, queryFn} = tasksQuery(scope)
			const unsubscribe = new QueryObserver(client, {queryKey, queryFn}).subscribe(() => {})
			await vi.waitFor(() => expect(client.getQueryData(queryKey)).toBeDefined())
			sdk.projectViewTasksList.mockClear()
			return {client, unsubscribe}
		}

		it.each([false, true])('does not refetch it when the project stays (optimistic: %s)', async optimistic => {
			const {client, unsubscribe} = await openAggregateList()
			sdk.patchTasksRead.mockResolvedValue({data: {id: 3, project_id: 101, done: true}})
			await client.getMutationCache()
				.build(client, updateTaskMutationOptions(optimistic))
				.execute({id: 3, project_id: 101, done: true})
			expect(sdk.projectViewTasksList).not.toHaveBeenCalled()
			unsubscribe()
		})

		it.each([false, true])('refetches it when the task moves (optimistic: %s)', async optimistic => {
			const {client, unsubscribe} = await openAggregateList()
			sdk.patchTasksRead.mockResolvedValue({data: {id: 3, project_id: 7}})
			await client.getMutationCache()
				.build(client, updateTaskMutationOptions(optimistic))
				.execute({id: 3, project_id: 7})
			expect(sdk.projectViewTasksList).toHaveBeenCalledOnce()
			unsubscribe()
		})
	})

	it('addresses the delete endpoint by task id', async () => {
		const client = new QueryClient()
		sdk.tasksDelete.mockResolvedValue({})
		await client.getMutationCache()
			.build(client, deleteTaskMutationOptions())
			.execute(1)
		expect(sdk.tasksDelete).toHaveBeenCalledWith({path: {task: 1}})
	})

	it('patches embedded assignees without creating an assignee cache', async () => {
		const client = new QueryClient()
		client.setQueryData(taskKeys.detail(1), {id: 1, assignees: []})
		sdk.taskAssigneesCreate.mockResolvedValue({data: {user_id: 2}})
		await client.getMutationCache()
			.build(client, addTaskAssigneeMutationOptions())
			.execute({taskId: 1, user: {id: 2, username: 'alice'}})
		expect(client.getQueryData<Task>(taskKeys.detail(1))?.assignees).toEqual([{id: 2, username: 'alice'}])
		expect(client.getQueryCache().getAll()).toHaveLength(1)
	})

	it('rolls back an optimistic bucket move when the request fails', async () => {
		const client = new QueryClient()
		const key = kanbanKeys.board(1, 2)
		const original: BoardData = {
			buckets: [
				normalizeBucket({id: 3, count: 1, tasks: [{id: 1, bucket_id: 3}]}),
				normalizeBucket({id: 4, count: 0, tasks: []}),
			],
			pages: {3: 1, 4: 1},
		}
		client.setQueryData(key, original)
		sdk.taskBucketUpdate.mockImplementation(() => {
			expect(client.getQueryData<BoardData>(key)?.buckets[1].tasks).toMatchObject([{id: 1, bucket_id: 4}])
			throw new Error('denied')
		})
		await expect(client.getMutationCache()
			.build(client, moveTaskMutationOptions())
			.execute({project: 1, view: 2, bucket: 4, task: {id: 1}})).rejects.toThrow('denied')
		expect(client.getQueryData(key)).toEqual(original)
	})

	it('rolls back only the caches holding the task', async () => {
		const client = new QueryClient()
		const key = taskKeys.list({project: 1})
		const other = taskKeys.list({project: 2})
		client.setQueryData(key, {items: [{id: 1, title: 'old'}], total: 1})
		client.setQueryData(other, {items: [{id: 9, title: 'other'}], total: 1})
		sdk.patchTasksRead.mockImplementation(() => {
			client.setQueryData(other, {items: [{id: 9, title: 'meanwhile'}], total: 1})
			throw new Error('denied')
		})
		await expect(client.getMutationCache()
			.build(client, updateTaskMutationOptions(true))
			.execute({id: 1, title: 'new'})).rejects.toThrow('denied')
		expect(client.getQueryData<PaginatedTask>(other)?.items).toEqual([{id: 9, title: 'meanwhile'}])
		expect(client.getQueryData<PaginatedTask>(key)?.items).toEqual([{id: 1, title: 'old'}])
	})

	it('leaves fetches of caches without the task running', async () => {
		const client = new QueryClient()
		const other = taskKeys.list({project: 2})
		client.setQueryData(taskKeys.list({project: 1}), {items: [{id: 1, title: 'old'}], total: 1})
		let release: (page: PaginatedTask) => void = () => {}
		const pending = client.fetchQuery({
			queryKey: other,
			queryFn: () => new Promise<PaginatedTask>(resolve => { release = resolve }),
		})
		sdk.patchTasksRead.mockResolvedValue({data: {id: 1, title: 'new'}})
		await client.getMutationCache()
			.build(client, updateTaskMutationOptions(true))
			.execute({id: 1, title: 'new'})
		release({items: [{id: 9}], total: 1})
		await expect(pending).resolves.toMatchObject({items: [{id: 9}]})
		expect(client.getQueryData<PaginatedTask>(other)?.items).toEqual([{id: 9}])
	})

	it('retains successful bulk batches and input alignment after partial failure', async () => {
		const client = new QueryClient()
		sdk.tasksBulkCreate.mockImplementation(({body}) => {
			if (body.tasks[0].title === '0') throw new Error('failed batch')
			return {data: {tasks: body.tasks.map((task: Task) => ({...task, id: 7}))}}
		})
		const input = Array.from({length: 101}, (_, index) => ({title: String(index), project_id: 1}))
		const result = await client.getMutationCache()
			.build(client, bulkCreateTasksMutationOptions())
			.execute(input)
		expect(result.tasks[100]).toMatchObject({id: 7, title: '100'})
		expect(result.tasks.slice(0, 100)).toEqual(Array(100).fill(null))
		expect(result.error).toBeInstanceOf(Error)
	})

	it('stops a bulk write before the next batch when the identity changes', async () => {
		const {assertClientRequestContext, isClientRequestContextCurrent} = await import('@/client/requestContext')
		const client = new QueryClient()
		sdk.tasksBulkCreate.mockImplementation(({body}) => {
			vi.mocked(isClientRequestContextCurrent).mockReturnValue(false)
			vi.mocked(assertClientRequestContext).mockImplementation(() => {
				throw new DOMException('Changed identity', 'AbortError')
			})
			return {data: {tasks: body.tasks.map((task: Task) => ({...task, id: 1}))}}
		})
		await expect(client.getMutationCache()
			.build(client, bulkCreateTasksMutationOptions())
			.execute(Array.from({length: 101}, (_, index) => ({title: String(index), project_id: 1}))))
			.rejects.toThrow('Changed identity')
		expect(sdk.tasksBulkCreate).toHaveBeenCalledTimes(1)
	})

	it('keeps the cached per-view position when the moved task comes back without one', async () => {
		const client = new QueryClient()
		const key = kanbanKeys.board(1, 2)
		client.setQueryData(key, {
			buckets: [
				{id: 3, count: 1, tasks: [{id: 1, position: 42, bucket_id: 3}]},
				{id: 4, count: 0, tasks: []},
			],
			pages: {3: 1, 4: 1},
		})
		sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1, position: 0}}})
		await client.getMutationCache()
			.build(client, moveTaskMutationOptions())
			.execute({project: 1, view: 2, bucket: 4, task: {id: 1}})
		expect(client.getQueryData<BoardData>(key)?.buckets[1].tasks)
			.toMatchObject([{id: 1, position: 42, bucket_id: 4}])
	})

	it('renames the moved view bucket on the cached task and leaves other views alone', async () => {
		const client = new QueryClient()
		const detail = taskKeys.detail(1, ['buckets'])
		client.setQueryData(detail, {
			id: 1,
			buckets: [
				{id: 3, project_view_id: 2, title: 'To-Do'},
				{id: 8, project_view_id: 5, title: 'Backlog'},
			],
		})
		client.setQueryData(kanbanKeys.board(1, 2), {
			buckets: [
				{id: 3, project_view_id: 2, title: 'To-Do', count: 1, tasks: [{id: 1}]},
				{id: 4, project_view_id: 2, title: 'Done', count: 0, tasks: []},
			],
			pages: {3: 1, 4: 1},
		})
		sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1}}})
		await client.getMutationCache()
			.build(client, moveTaskMutationOptions())
			.execute({project: 1, view: 2, bucket: 4, task: {id: 1}})
		expect(client.getQueryData<Task>(detail)?.buckets).toMatchObject([
			{id: 4, project_view_id: 2, title: 'Done'},
			{id: 8, project_view_id: 5, title: 'Backlog'},
		])
	})

	describe('moving a task that a parent project board shows as a sub-project card', () => {
		async function openBoards() {
			sdk.projectViewBucketsTasksList.mockResolvedValue({
				data: {items: [{id: 3, tasks: [{id: 1, project_id: 101}]}, {id: 4, tasks: []}]},
			})
			const client = new QueryClient()
			const boards = {
				parent: kanbanQuery(100, 10, {include_subprojects: true}),
				child: kanbanQuery(101, 11),
				otherAggregate: kanbanQuery(101, 12, {include_subprojects: true}),
			}
			const detailFn = vi.fn(() => ({id: 1, project_id: 101}))
			const detail = taskKeys.detail(1)
			const unsubscribe = [
				...Object.values(boards).map(({queryKey, queryFn}) =>
					new QueryObserver(client, {queryKey, queryFn}).subscribe(() => {})),
				new QueryObserver(client, {queryKey: detail, queryFn: detailFn}).subscribe(() => {}),
			]
			await vi.waitFor(() => {
				expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(3)
				expect(detailFn).toHaveBeenCalledOnce()
			})
			sdk.projectViewBucketsTasksList.mockClear()
			return {client, detailFn, unsubscribe}
		}

		it('reads the parent board and the detail again when it is moved on the parent board', async () => {
			const {client, detailFn, unsubscribe} = await openBoards()
			sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1, project_id: 101}}})
			await client.getMutationCache()
				.build(client, moveTaskMutationOptions())
				.execute({project: 100, view: 10, bucket: 4, task: {id: 1, project_id: 101}})
			// The board of the task's own project is not an aggregate and is patched by the move itself.
			await vi.waitFor(() => expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1))
			expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledWith(expect.objectContaining({
				path: {project: 100, view: 10},
			}))
			await vi.waitFor(() => expect(detailFn).toHaveBeenCalledTimes(2))
			unsubscribe.forEach(stop => stop())
		})

		it('reads the parent board again but not the detail when it is moved on its own board', async () => {
			const {client, detailFn, unsubscribe} = await openBoards()
			sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1, project_id: 101}}})
			await client.getMutationCache()
				.build(client, moveTaskMutationOptions())
				.execute({project: 101, view: 11, bucket: 4, task: {id: 1, project_id: 101}})
			await vi.waitFor(() => expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledWith(
				expect.objectContaining({path: {project: 100, view: 10}}),
			))
			// A board of the task's own project shows the card as its own and is patched locally.
			expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1)
			expect(detailFn).toHaveBeenCalledOnce()
			unsubscribe.forEach(stop => stop())
		})

		it('patches the parent board only when its own task moves on it', async () => {
			const {client, unsubscribe} = await openBoards()
			sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1, project_id: 100}}})
			await client.getMutationCache()
				.build(client, moveTaskMutationOptions())
				.execute({project: 100, view: 10, bucket: 4, task: {id: 1, project_id: 100}})
			expect(sdk.projectViewBucketsTasksList).not.toHaveBeenCalledWith(expect.objectContaining({
				path: {project: 100, view: 10},
			}))
			unsubscribe.forEach(stop => stop())
		})
	})

	it('keeps the buckets of a task in a list when an update response and the edited task carry none', async () => {
		const client = new QueryClient()
		const key = taskKeys.list({params: {expand: ['buckets']}})
		const buckets = [{id: 3, project_view_id: 2, title: 'Doing'}]
		client.setQueryData(key, {items: [{id: 1, title: 'old', buckets}], total: 1, total_pages: 1})
		sdk.patchTasksRead.mockResolvedValue({data: {id: 1, title: 'new'}})
		await client.getMutationCache()
			.build(client, updateTaskMutationOptions(true))
			.execute({id: 1, title: 'new', buckets: []})
		expect(client.getQueryData<PaginatedTask>(key)?.items).toMatchObject([{id: 1, title: 'new', buckets}])
	})

	it('renames the moved view bucket on a task in a list', async () => {
		const client = new QueryClient()
		const key = taskKeys.list({params: {expand: ['buckets']}})
		client.setQueryData(key, {
			items: [{id: 1, buckets: [{id: 3, project_view_id: 2, title: 'To-Do'}]}],
			total: 1,
			total_pages: 1,
		})
		client.setQueryData(kanbanKeys.board(1, 2), {
			buckets: [
				{id: 3, project_view_id: 2, title: 'To-Do', count: 1, tasks: [{id: 1}]},
				{id: 4, project_view_id: 2, title: 'Doing', count: 0, tasks: []},
			],
			pages: {3: 1, 4: 1},
		})
		sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1}}})
		await client.getMutationCache()
			.build(client, moveTaskMutationOptions())
			.execute({project: 1, view: 2, bucket: 4, task: {id: 1}})
		expect(client.getQueryData<PaginatedTask>(key)?.items).toMatchObject([
			{id: 1, buckets: [{id: 4, project_view_id: 2, title: 'Doing'}]},
		])
	})

	it('renames the moved view bucket from the cached bucket list when no board is cached', async () => {
		const client = new QueryClient()
		const detail = taskKeys.detail(1, ['buckets'])
		const queryFn = vi.fn(() => ({id: 1, buckets: [{id: 3, project_view_id: 2, title: 'To-Do'}]}))
		const unsubscribe = new QueryObserver(client, {queryKey: detail, queryFn}).subscribe(() => {})
		await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
		client.setQueryData(bucketKeys.list(1, 2), [
			{id: 3, project_view_id: 2, title: 'To-Do'},
			{id: 4, project_view_id: 2, title: 'Done'},
		])
		sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1}}})
		await client.getMutationCache()
			.build(client, moveTaskMutationOptions())
			.execute({project: 1, view: 2, bucket: 4, task: {id: 1}})
		expect(client.getQueryData<Task>(detail)?.buckets).toMatchObject([
			{id: 4, project_view_id: 2, title: 'Done'},
		])
		expect(queryFn).toHaveBeenCalledTimes(1)
		unsubscribe()
	})

	it('refetches the watched task detail when neither the board nor the bucket list is cached', async () => {
		const client = new QueryClient()
		const queryFn = vi.fn(() => ({id: 1, buckets: [{id: 3, project_view_id: 2, title: 'To-Do'}]}))
		const observer = new QueryObserver(client, {queryKey: taskKeys.detail(1, ['buckets']), queryFn})
		const unsubscribe = observer.subscribe(() => {})
		await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
		sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 4, task: {id: 1}}})
		await client.getMutationCache()
			.build(client, moveTaskMutationOptions())
			.execute({project: 1, view: 2, bucket: 4, task: {id: 1}})
		await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2))
		unsubscribe()
	})

	it('reorders only cached lists for the affected view', async () => {
		const client = new QueryClient()
		const key = taskKeys.list({project: 1, view: 2, params: {sort_by: ['position']}})
		const other = taskKeys.list({project: 1, view: 3, params: {sort_by: ['position']}})
		const data = {items: [{id: 1, position: 1}, {id: 2, position: 2}], total: 2}
		client.setQueryData(key, data)
		client.setQueryData(other, data)
		sdk.tasksPositionUpdate.mockResolvedValue({data: {position: 3}})
		await client.getMutationCache()
			.build(client, updateTaskPositionMutationOptions())
			.execute({taskId: 1, project_view_id: 2, position: 3})
		expect(client.getQueryData<PaginatedTask>(key)?.items?.map(task => task.id)).toEqual([2, 1])
		expect(client.getQueryData(other)).toEqual(data)
	})

	it('reorders descending lists in their own direction and leaves unsorted ones alone', async () => {
		const client = new QueryClient()
		const descending = taskKeys.list({
			project: 1,
			view: 2,
			params: {sort_by: ['position'], order_by: ['desc']},
		})
		const search = taskKeys.list({project: 1, view: 2, params: {q: 'term'}})
		const data = {items: [{id: 2, position: 2}, {id: 1, position: 1}], total: 2}
		client.setQueryData(descending, data)
		client.setQueryData(search, data)
		sdk.tasksPositionUpdate.mockResolvedValue({data: {position: 3}})
		await client.getMutationCache()
			.build(client, updateTaskPositionMutationOptions())
			.execute({taskId: 1, project_view_id: 2, position: 3})
		expect(client.getQueryData<PaginatedTask>(descending)?.items?.map(task => task.id)).toEqual([1, 2])
		expect(client.getQueryData<PaginatedTask>(search)?.items?.map(task => task.id)).toEqual([2, 1])
	})

	it('serializes clearing a date and relative reminders as valid API dates', () => {
		expect(taskWriteBody({
			due_date: '',
			reminders: [{relative_to: 'due_date', relative_period: -60, reminder: ''}],
		})).toEqual({
			due_date: '0001-01-01T00:00:00Z',
			reminders: [{
				relative_to: 'due_date',
				relative_period: -60,
				reminder: '0001-01-01T00:00:00Z',
			}],
		})
	})

	it('leaves boards without the completed task untouched and unfetched', async () => {
		const {projectKeys} = await import('./projects')
		const client = new QueryClient()
		const otherKey = kanbanKeys.board(1, 5)
		const otherBoard: BoardData = {
			buckets: [
				normalizeBucket({id: 6, count: 0, tasks: []}),
				normalizeBucket({id: 7, count: 0, tasks: []}),
			],
			pages: {6: 1, 7: 1},
		}
		client.setQueryData(projectKeys.detail(1), {
			id: 1,
			views: [{id: 2, done_bucket_id: 4}, {id: 5, done_bucket_id: 7}],
		})
		client.setQueryData(kanbanKeys.board(1, 2), {
			buckets: [
				{id: 3, count: 1, tasks: [{id: 1, done: false}]},
				{id: 4, count: 0, tasks: []},
			],
			pages: {3: 1, 4: 1},
		})
		client.setQueryData(otherKey, otherBoard)
		sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: true}})
		await client.getMutationCache()
			.build(client, updateTaskMutationOptions())
			.execute({id: 1, done: true})
		// structural sharing hides no-op rewrites, so count writes instead: 1 = seed only
		expect(client.getQueryState(otherKey)?.dataUpdateCount).toBe(1)
		expect(sdk.projectViewBucketsTasksList).not.toHaveBeenCalled()
	})

	it('inserts a created card into a scrolled board without refetching it, while lists still refetch', async () => {
		const {client, board, list, unsubscribe} = await seedScrolledBoard()
		sdk.tasksCreate.mockResolvedValue({data: {id: 99, title: 'new', project_id: 1, bucket_id: 3}})
		await client.getMutationCache()
			.build(client, createTaskMutationOptions())
			.execute({title: 'new', project_id: 1, bucket_id: 3})
		const data = client.getQueryData<BoardData>(board.queryKey)!
		expect(data.buckets[0].tasks[0]).toMatchObject({id: 99, bucket_id: 3})
		expect(data.buckets[0].tasks).toHaveLength(31)
		expect(data.buckets[0].count).toBe(31)
		expect(data.pages).toEqual({3: 2})
		expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledOnce()
		expect(sdk.projectTasksList).toHaveBeenCalledTimes(2)
		expect(client.getQueryState(board.queryKey)?.isInvalidated).toBe(true)
		expect(client.getQueryState(list.queryKey)?.isInvalidated).toBe(false)
		unsubscribe()
	})

	it('drops a deleted card from a scrolled board without refetching it', async () => {
		const {client, board, unsubscribe} = await seedScrolledBoard()
		sdk.tasksDelete.mockResolvedValue({})
		await client.getMutationCache()
			.build(client, deleteTaskMutationOptions())
			.execute(1)
		const data = client.getQueryData<BoardData>(board.queryKey)!
		expect(data.buckets[0].tasks.map(task => task.id)).not.toContain(1)
		expect(data.buckets[0].count).toBe(29)
		expect(data.pages).toEqual({3: 2})
		expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledOnce()
		expect(sdk.projectTasksList).toHaveBeenCalledTimes(2)
		unsubscribe()
	})

	it('inserts a duplicate into the default bucket of every cached view', async () => {
		const client = await seedDefaultBucketBoards()
		sdk.tasksDuplicate.mockResolvedValue({data: {duplicated_task: {id: 9, project_id: 1, bucket_id: 0}}})
		await client.getMutationCache()
			.build(client, duplicateTaskMutationOptions())
			.execute(1)
		expect(client.getQueryData<BoardData>(kanbanKeys.board(1, 2))?.buckets
			.map(bucket => bucket.tasks.map(task => task.id))).toEqual([[], [9]])
		expect(client.getQueryData<BoardData>(kanbanKeys.board(1, 5))?.buckets[0].tasks
			.map(task => task.id)).toEqual([9])
	})

	it('caches a created card with its stored position in each view', async () => {
		const client = await seedDefaultBucketBoards()
		sdk.tasksCreate.mockResolvedValue({data: {id: 9, project_id: 1, position: 0, positions: [
			{task_id: 9, project_view_id: 2, position: 250},
			{task_id: 9, project_view_id: 5, position: 100},
		]}})
		await client.getMutationCache()
			.build(client, createTaskMutationOptions())
			.execute({title: 'new', project_id: 1})
		expect(client.getQueryData<BoardData>(kanbanKeys.board(1, 2))?.buckets[1].tasks[0].position).toBe(250)
		expect(client.getQueryData<BoardData>(kanbanKeys.board(1, 5))?.buckets[0].tasks[0].position).toBe(100)
	})

	it('keeps the input order of a bulk batch by inserting it back to front', async () => {
		const client = await seedDefaultBucketBoards()
		sdk.tasksBulkCreate.mockImplementation(({body}) => ({
			data: {
				tasks: body.tasks.map((task: Task, index: number) => ({
					...task,
					id: 20 + index,
					project_id: 1,
				})),
			},
		}))
		await client.getMutationCache()
			.build(client, bulkCreateTasksMutationOptions())
			.execute([
				{title: 'a', project_id: 1},
				{title: 'b', project_id: 1},
			])
		expect(client.getQueryData<BoardData>(kanbanKeys.board(1, 2))?.buckets[1]).toMatchObject({
			count: 2,
			tasks: [{id: 20}, {id: 21}],
		})
	})

	it('leaves a filter-configured board to its stale mark', async () => {
		const {projectKeys} = await import('./projects')
		const client = new QueryClient()
		const filtered = kanbanKeys.board(1, 2)
		const manual = kanbanKeys.board(1, 5)
		const board = {buckets: [normalizeBucket({id: 3, count: 0, tasks: []})], pages: {3: 1}}
		client.setQueryData(projectKeys.detail(1), {
			id: 1,
			views: [
				{id: 2, bucket_configuration_mode: 'filter'},
				{id: 5, bucket_configuration_mode: 'manual'},
			],
		})
		client.setQueryData(filtered, board)
		client.setQueryData(manual, {
			buckets: [normalizeBucket({id: 6, count: 0, tasks: []})],
			pages: {6: 1},
		})
		sdk.tasksCreate.mockResolvedValue({data: {id: 99, project_id: 1}})
		await client.getMutationCache()
			.build(client, createTaskMutationOptions())
			.execute({title: 'new', project_id: 1})
		expect(client.getQueryData<BoardData>(filtered)).toEqual(board)
		expect(client.getQueryState(filtered)?.isInvalidated).toBe(true)
		expect(client.getQueryData<BoardData>(manual)?.buckets[0].tasks.map(task => task.id)).toEqual([99])
	})

	it('renames the cached task bucket when toggling done moves the card', async () => {
		const {projectKeys} = await import('./projects')
		const client = new QueryClient()
		const detail = taskKeys.detail(1, ['buckets'])
		client.setQueryData(projectKeys.detail(1), {
			id: 1,
			views: [{id: 2, done_bucket_id: 4, default_bucket_id: 3}],
		})
		client.setQueryData(kanbanKeys.board(1, 2), {
			buckets: [
				{id: 3, project_view_id: 2, title: 'To-Do', count: 1, tasks: [{id: 1, done: false}]},
				{id: 4, project_view_id: 2, title: 'Done', count: 0, tasks: []},
			],
			pages: {3: 1, 4: 1},
		})
		client.setQueryData(detail, {
			id: 1,
			done: false,
			buckets: [{id: 3, project_view_id: 2, title: 'To-Do'}],
		})
		sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: true}})
		await client.getMutationCache()
			.build(client, updateTaskMutationOptions())
			.execute({id: 1, done: true})
		expect(client.getQueryData<Task>(detail)?.buckets).toMatchObject([
			{id: 4, project_view_id: 2, title: 'Done'},
		])
		sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: false}})
		await client.getMutationCache()
			.build(client, updateTaskMutationOptions())
			.execute({id: 1, done: false})
		expect(client.getQueryData<Task>(detail)?.buckets).toMatchObject([
			{id: 3, project_view_id: 2, title: 'To-Do'},
		])
	})

	describe('toggling done on a task in a list without a board', () => {
		const viewBuckets = [
			{id: 7, project_view_id: 2, title: 'To-Do'},
			{id: 8, project_view_id: 2, title: 'Doing'},
			{id: 9, project_view_id: 2, title: 'Done'},
		]

		async function seedList(bucket: number) {
			const {projectKeys} = await import('./projects')
			const client = new QueryClient()
			client.setQueryData(projectKeys.list(), {
				projects: [{id: 1, views: [{id: 2, default_bucket_id: 7, done_bucket_id: 9}]}],
				favoriteProject: null,
				savedFilterProjects: [],
			})
			const key = taskKeys.list({project: 1, view: 1, params: {expand: ['buckets']}})
			client.setQueryData(key, {
				items: [{id: 1, project_id: 1, done: bucket === 9, buckets: [viewBuckets[bucket - 7]]}],
				total: 1,
				total_pages: 1,
			})
			sdk.bucketsList.mockResolvedValue({data: {items: viewBuckets}})
			return {client, key}
		}

		const listBucketTitle = (client: QueryClient, key: ReturnType<typeof taskKeys.list>) =>
			client.getQueryData<PaginatedTask>(key)?.items?.[0]?.buckets?.[0]?.title

		it('shows the done bucket when the task is marked done', async () => {
			const {client, key} = await seedList(8)
			sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: true}})
			await client.getMutationCache()
				.build(client, updateTaskMutationOptions(true))
				.execute({id: 1, project_id: 1, done: true})
			expect(listBucketTitle(client, key)).toBe('Done')
			expect(sdk.bucketsList).toHaveBeenCalledWith(expect.objectContaining({path: {project: 1, view: 2}}))
		})

		it('shows the default bucket when the task is marked undone', async () => {
			const {client, key} = await seedList(9)
			sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: false}})
			await client.getMutationCache()
				.build(client, updateTaskMutationOptions(true))
				.execute({id: 1, project_id: 1, done: false})
			expect(listBucketTitle(client, key)).toBe('To-Do')
		})

		it('does not load the buckets when the task already sits in the target bucket', async () => {
			const {client} = await seedList(9)
			sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: true}})
			await client.getMutationCache()
				.build(client, updateTaskMutationOptions(true))
				.execute({id: 1, project_id: 1, done: true})
			expect(sdk.bucketsList).not.toHaveBeenCalled()
		})

		it('leaves the cached bucket alone when the bucket list cannot be loaded', async () => {
			const {client, key} = await seedList(8)
			sdk.bucketsList.mockRejectedValue(new Error('offline'))
			sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: true}})
			await client.getMutationCache()
				.build(client, updateTaskMutationOptions(true))
				.execute({id: 1, project_id: 1, done: true})
			expect(listBucketTitle(client, key)).toBe('Doing')
		})
	})

	it('renames the done bucket on a detail opened without a board from the cached bucket list', async () => {
		const {projectKeys} = await import('./projects')
		const client = new QueryClient()
		const detail = taskKeys.detail(1, ['buckets'])
		const queryFn = vi.fn(() => ({
			id: 1,
			project_id: 1,
			done: false,
			buckets: [{id: 3, project_view_id: 2, title: 'To-Do'}],
		}))
		const unsubscribe = new QueryObserver(client, {queryKey: detail, queryFn}).subscribe(() => {})
		await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
		client.setQueryData(projectKeys.detail(1), {
			id: 1,
			views: [{id: 2, default_bucket_id: 3, done_bucket_id: 4}],
		})
		client.setQueryData(bucketKeys.list(1, 2), [
			{id: 3, project_view_id: 2, title: 'To-Do'},
			{id: 4, project_view_id: 2, title: 'Done'},
		])
		sdk.patchTasksRead.mockResolvedValue({data: {id: 1, project_id: 1, done: true}})
		await client.getMutationCache()
			.build(client, updateTaskMutationOptions())
			.execute({id: 1, done: true})
		expect(client.getQueryData<Task>(detail)?.buckets).toMatchObject([
			{id: 4, project_view_id: 2, title: 'Done'},
		])
		expect(queryFn).toHaveBeenCalledTimes(1)
		expect(sdk.projectViewBucketsTasksList).not.toHaveBeenCalled()
		expect(sdk.bucketsList).not.toHaveBeenCalled()
		unsubscribe()
	})
})
