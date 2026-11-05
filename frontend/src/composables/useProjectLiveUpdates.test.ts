import type {Ref} from 'vue'
import {
	defineComponent,
	nextTick,
} from 'vue'
import {
	flushPromises,
	mount,
} from '@vue/test-utils'
import {
	QueryClient,
	QueryObserver,
	VueQueryPlugin,
	mutationOptions,
	type QueryKey,
} from '@tanstack/vue-query'
import {
	afterEach,
	beforeEach,
	expect,
	it,
	vi,
} from 'vitest'
import {useProjectLiveUpdates} from './useProjectLiveUpdates'
import {
	bucketKeys,
	kanbanKeys,
	kanbanMutationKeys,
	type BoardData,
} from '@/client/queries/kanban'
import {projectKeys} from '@/client/queries/projects'
import {
	taskKeys,
	type TaskResponse,
} from '@/client/queries/tasks'

const sdk = vi.hoisted(() => ({
	bucketsList: vi.fn(),
	projectViewBucketsTasksList: vi.fn(),
	projectViewTasksList: vi.fn(),
}))

vi.mock('@/client/generated', () => sdk)

const ws = vi.hoisted(() => ({
	handlers: new Map<string, (message: {data: unknown}) => void>(),
	unsubscribed: [] as string[],
	refs: undefined as undefined | {
		authenticated: Ref<boolean>,
		mayHaveMissedEvents: Ref<boolean>,
	},
}))

vi.mock('./useWebSocket', async () => {
	// vi.mock 的工厂会在静态导入求值前运行，只能动态取 vue。
	const {ref} = await import('vue')
	const authenticated = ref(true)
	const mayHaveMissedEvents = ref(false)
	ws.refs = {
		authenticated,
		mayHaveMissedEvents,
	}

	return {
		useWebSocket: () => ({
			subscribe: (event: string, callback: (message: {data: unknown}) => void) => {
				ws.handlers.set(event, callback)
				return () => {
					ws.handlers.delete(event)
					ws.unsubscribed.push(event)
				}
			},
			authenticated,
			mayHaveMissedEvents,
		}),
	}
})

const FLUSH_DELAY_MS = 300

let client: QueryClient
let observers: (() => void)[]

beforeEach(() => {
	vi.useFakeTimers()
	ws.handlers.clear()
	ws.unsubscribed = []
	sdk.projectViewBucketsTasksList.mockReset()
	sdk.projectViewTasksList.mockReset()
	client = new QueryClient()
	observers = []
})

afterEach(() => {
	vi.useRealTimers()
	for (const unsubscribe of observers) unsubscribe()
	client.clear()
	if (ws.refs) {
		ws.refs.authenticated.value = true
		ws.refs.mayHaveMissedEvents.value = false
	}
})

function mountUpdates() {
	return mount(defineComponent({
		setup() {
			useProjectLiveUpdates()
			return () => null
		},
	}), {global: {plugins: [[VueQueryPlugin, {queryClient: client}]]}})
}

function emit(data: unknown) {
	ws.handlers.get('project.changed')!({data})
}

const isInvalidated = (key: Parameters<QueryClient['getQueryState']>[0]) =>
	client.getQueryState(key)?.isInvalidated === true

const taskOf = (id: number) => ({id} as TaskResponse)

function boardWith(bucketID: number, tasks: TaskResponse[], pages = 1): BoardData {
	return {
		buckets: [{
			id: bucketID,
			title: 'Bucket',
			count: tasks.length,
			limit: 0,
			position: 1,
			tasks,
		}],
		pages: {[bucketID]: pages},
	}
}

function emptyBoard(): BoardData {
	return {
		buckets: [],
		pages: {},
	}
}

// 只登记观察者，不发请求：有观察者的看板才会被重新拉取。
const observe = (key: QueryKey) => {
	const unsubscribe = new QueryObserver(client, {
		queryKey: key,
		queryFn: () => Promise.resolve(undefined),
		enabled: false,
	}).subscribe(() => {})
	observers.push(unsubscribe)
}

it('keeps the loaded pages of every bucket when it refetches a board', async () => {
	const boardKey = kanbanKeys.board(1, 4)
	client.setQueryData<BoardData>(boardKey, boardWith(3, [taskOf(1), taskOf(2)], 2))
	sdk.projectViewBucketsTasksList.mockResolvedValue({
		data: {
			items: [{
				id: 3,
				title: 'Bucket',
				count: 30,
				position: 1,
				tasks: [{id: 1}, {id: 2}],
			}],
		},
	})
	sdk.projectViewTasksList.mockResolvedValue({data: {items: [{id: 9}]}})
	observe(boardKey)
	mountUpdates()

	emit({
		project_ids: [1],
		task_id: 0,
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	expect(sdk.projectViewTasksList).toHaveBeenCalledTimes(1)
	expect(sdk.projectViewTasksList).toHaveBeenCalledWith(expect.objectContaining({
		path: {
			project: 1,
			view: 4,
		},
		query: expect.objectContaining({
			page: 2,
			filter: 'bucket_id = 3',
		}),
	}))
	const board = client.getQueryData<BoardData>(boardKey)!
	expect(board.pages).toEqual({3: 2})
	expect(board.buckets[0].tasks.map(task => task.id)).toEqual([1, 2, 9])
})

it('counts only the pages it could load, so the next scroll does not skip one', async () => {
	const boardKey = kanbanKeys.board(1, 4)
	client.setQueryData<BoardData>(boardKey, boardWith(3, [taskOf(1)], 3))
	sdk.projectViewBucketsTasksList.mockResolvedValue({
		data: {
			items: [
				{
					id: 3,
					count: 30,
					tasks: [{id: 1}],
				},
			],
		},
	})
	sdk.projectViewTasksList
		.mockResolvedValueOnce({data: {items: [{id: 2}]}})
		.mockRejectedValueOnce(new Error('offline'))
	observe(boardKey)
	mountUpdates()

	emit({
		project_ids: [1],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	const board = client.getQueryData<BoardData>(boardKey)!
	expect(board.pages).toEqual({3: 2})
	expect(board.buckets[0].tasks.map(task => task.id)).toEqual([1, 2])
})

it('stops at the first failed page so a later page is requested again', async () => {
	const boardKey = kanbanKeys.board(1, 4)
	client.setQueryData<BoardData>(boardKey, boardWith(3, [taskOf(1)], 3))
	sdk.projectViewBucketsTasksList.mockResolvedValue({
		data: {
			items: [
				{
					id: 3,
					count: 30,
					tasks: [{id: 1}],
				},
			],
		},
	})
	// 第二页失败、第三页成功：第三页不能算进来，否则下次滚动会跳过第二页。
	sdk.projectViewTasksList
		.mockRejectedValueOnce(new Error('offline'))
		.mockResolvedValueOnce({data: {items: [{id: 3}]}})
	observe(boardKey)
	mountUpdates()

	emit({
		project_ids: [1],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	const board = client.getQueryData<BoardData>(boardKey)!
	expect(board.pages).toEqual({3: 1})
	expect(board.buckets[0].tasks.map(task => task.id)).toEqual([1])
})

it('serializes refreshes of one board and keeps the newer response', async () => {
	const boardKey = kanbanKeys.board(1, 4)
	client.setQueryData<BoardData>(boardKey, boardWith(3, [taskOf(1)]))
	const first = Promise.withResolvers<{data: {items: unknown[]}}>()
	const second = Promise.withResolvers<{data: {items: unknown[]}}>()
	sdk.projectViewBucketsTasksList
		.mockReturnValueOnce(first.promise)
		.mockReturnValueOnce(second.promise)
	observe(boardKey)
	mountUpdates()

	emit({
		project_ids: [1],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)
	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1)

	// 第一次刷新还没回来，再来的请求只能补刷一次，不能并发再发一次请求。
	emit({
		project_ids: [1],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)
	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1)

	first.resolve({data: {items: [{id: 3, count: 1, tasks: [{id: 1}]}]}})
	await flushPromises()
	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(2)

	// 旧响应先写回，补刷的新响应最后写回。
	second.resolve({data: {items: [{id: 3, count: 1, tasks: [{id: 2}]}]}})
	await flushPromises()
	expect(client.getQueryData<BoardData>(boardKey)!.buckets[0].tasks.map(task => task.id))
		.toEqual([2])
})

it('does not write a board back while a kanban mutation is running', async () => {
	const boardKey = kanbanKeys.board(1, 4)
	client.setQueryData<BoardData>(boardKey, boardWith(3, [taskOf(1)]))
	sdk.projectViewBucketsTasksList.mockResolvedValue({
		data: {
			items: [
				{
					id: 3,
					count: 1,
					tasks: [{id: 9}],
				},
			],
		},
	})
	observe(boardKey)
	mountUpdates()

	const pending = Promise.withResolvers<void>()
	const mutation = client.getMutationCache().build(client, mutationOptions({
		mutationKey: kanbanMutationKeys.all,
		mutationFn: () => pending.promise,
	}))
	const running = mutation.execute()
	await flushPromises()

	emit({
		project_ids: [1],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)
	await flushPromises()

	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1)
	expect(client.getQueryData<BoardData>(boardKey)!.buckets[0].tasks.map(task => task.id))
		.toEqual([1])

	pending.resolve()
	await running
	await flushPromises()

	// mutation 结束后补刷一次，这时才写回。
	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(2)
	expect(client.getQueryData<BoardData>(boardKey)!.buckets[0].tasks.map(task => task.id))
		.toEqual([9])
})

it('keeps a page that was loaded while the refresh was in flight', async () => {
	const boardKey = kanbanKeys.board(1, 4)
	client.setQueryData<BoardData>(boardKey, boardWith(3, [taskOf(1)]))
	const response = Promise.withResolvers<{data: {items: unknown[]}}>()
	sdk.projectViewBucketsTasksList.mockReturnValueOnce(response.promise)
	sdk.projectViewTasksList.mockResolvedValue({data: {items: [{id: 5}]}})
	observe(boardKey)
	mountUpdates()

	emit({
		project_ids: [1],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)
	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1)

	// 刷新还没回来时用户滚出了第二页，写回时不能把 pages 退回 1。
	client.setQueryData<BoardData>(boardKey, current => current ? {...current, pages: {3: 2}} : current)
	response.resolve({data: {items: [{id: 3, count: 2, tasks: [{id: 1}]}]}})
	await flushPromises()

	expect(sdk.projectViewTasksList).toHaveBeenCalledTimes(1)
	expect(sdk.projectViewTasksList).toHaveBeenCalledWith(expect.objectContaining({
		query: expect.objectContaining({page: 2}),
	}))
	const board = client.getQueryData<BoardData>(boardKey)!
	expect(board.pages).toEqual({3: 2})
	expect(board.buckets[0].tasks.map(task => task.id)).toEqual([1, 5])
})

it('only refreshes the collections holding the task for a content event', async () => {
	const holdingKey = kanbanKeys.board(1, 4)
	const otherKey = kanbanKeys.board(2, 8)
	client.setQueryData<BoardData>(holdingKey, boardWith(3, [taskOf(5)]))
	client.setQueryData<BoardData>(otherKey, boardWith(7, [taskOf(6)]))
	client.setQueryData<TaskResponse>(taskKeys.detail(5, []), taskOf(5))
	sdk.projectViewBucketsTasksList.mockResolvedValue({
		data: {
			items: [
				{
					id: 3,
					count: 1,
					tasks: [{id: 5}],
				},
			],
		},
	})
	observe(holdingKey)
	mountUpdates()

	emit({
		project_ids: [1],
		task_id: 5,
		kind: 'content',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1)
	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledWith(expect.objectContaining({
		path: {
			project: 1,
			view: 4,
		},
	}))
	expect(isInvalidated(otherKey)).toBe(false)
	expect(isInvalidated(taskKeys.detail(5, []))).toBe(true)
})

it('refreshes the boards and lists of every project named by the event', async () => {
	client.setQueryData<BoardData>(kanbanKeys.board(3, 12), emptyBoard())
	client.setQueryData<BoardData>(kanbanKeys.board(7, 28), emptyBoard())
	client.setQueryData(taskKeys.list({project: 3}), {items: []})
	mountUpdates()

	emit({
		project_ids: [40, 3],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	expect(isInvalidated(kanbanKeys.board(3, 12))).toBe(true)
	expect(isInvalidated(taskKeys.list({project: 3}))).toBe(true)
	expect(isInvalidated(kanbanKeys.board(7, 28))).toBe(false)
})

it('treats saved filter boards and lists as affected', async () => {
	const filterBoard = kanbanKeys.board(-5, 1)
	const filterList = taskKeys.list({
		project: -5,
		view: 1,
	})
	client.setQueryData<BoardData>(filterBoard, emptyBoard())
	client.setQueryData(filterList, {items: []})
	mountUpdates()

	emit({
		project_ids: [1],
		kind: 'layout',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	expect(isInvalidated(filterBoard)).toBe(true)
	expect(isInvalidated(filterList)).toBe(true)
})

it('invalidates the project and its views on a project event', async () => {
	client.setQueryData(projectKeys.detail(1), {id: 1})
	client.setQueryData(bucketKeys.list(1, 4), [])
	mountUpdates()

	emit({
		project_ids: [1],
		kind: 'project',
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	expect(isInvalidated(projectKeys.detail(1))).toBe(true)
	expect(isInvalidated(bucketKeys.list(1, 4))).toBe(true)
})

it('refetches the visible board and lists after a reconnect', async () => {
	const boardKey = kanbanKeys.board(1, 4)
	client.setQueryData<BoardData>(boardKey, emptyBoard())
	client.setQueryData(taskKeys.list({project: 1}), {items: []})
	sdk.projectViewBucketsTasksList.mockResolvedValue({data: {items: []}})
	observe(boardKey)
	mountUpdates()

	ws.refs!.authenticated.value = false
	ws.refs!.mayHaveMissedEvents.value = true
	await nextTick()
	ws.refs!.authenticated.value = true
	await nextTick()
	await flushPromises()

	expect(sdk.projectViewBucketsTasksList).toHaveBeenCalledTimes(1)
	expect(isInvalidated(taskKeys.list({project: 1}))).toBe(true)
})

it('merges consecutive events into a single flush', async () => {
	client.setQueryData<BoardData>(kanbanKeys.board(1, 4), emptyBoard())
	client.setQueryData<BoardData>(kanbanKeys.board(2, 8), emptyBoard())
	mountUpdates()

	emit({project_ids: [1]})
	emit({project_ids: [2]})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS - 1)

	expect(isInvalidated(kanbanKeys.board(1, 4))).toBe(false)

	await vi.advanceTimersByTimeAsync(1)

	expect(isInvalidated(kanbanKeys.board(1, 4))).toBe(true)
	expect(isInvalidated(kanbanKeys.board(2, 8))).toBe(true)
})

it('unsubscribes and drops a pending flush when the scope is disposed', async () => {
	client.setQueryData(taskKeys.list({project: 1}), {items: []})
	const wrapper = mountUpdates()

	expect(ws.handlers.has('project.changed')).toBe(true)
	emit({project_ids: [1]})
	wrapper.unmount()

	expect(ws.unsubscribed).toEqual(['project.changed'])
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)
	await flushPromises()
	expect(isInvalidated(taskKeys.list({project: 1}))).toBe(false)
})

it('ignores events without projects or tasks', async () => {
	client.setQueryData<BoardData>(kanbanKeys.board(1, 4), emptyBoard())
	mountUpdates()

	emit(null)
	emit({
		project_ids: [],
		task_id: 0,
	})
	await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS)

	expect(isInvalidated(kanbanKeys.board(1, 4))).toBe(false)
})
