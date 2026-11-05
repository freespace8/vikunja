import {
	afterEach,
	beforeEach,
	expect,
	it,
	vi,
} from 'vitest'
import {
	defineComponent,
} from 'vue'
import {
	flushPromises,
	mount,
} from '@vue/test-utils'
import {
	QueryClient,
	QueryObserver,
	VueQueryPlugin,
} from '@tanstack/vue-query'
import {useProjectLiveUpdates} from './useProjectLiveUpdates'
import {kanbanKeys} from '@/client/queries/kanban'
import {taskKeys} from '@/client/queries/tasks'

const ws = vi.hoisted(() => ({
	handlers: new Map<string, (message: {data: unknown}) => void>(),
	unsubscribed: [] as string[],
}))

vi.mock('./useWebSocket', () => ({
	useWebSocket: () => ({
		subscribe: (event: string, callback: (message: {data: unknown}) => void) => {
			ws.handlers.set(event, callback)
			return () => {
				ws.handlers.delete(event)
				ws.unsubscribed.push(event)
			}
		},
	}),
}))

let client: QueryClient

beforeEach(() => {
	ws.handlers.clear()
	ws.unsubscribed = []
	client = new QueryClient()
})

afterEach(() => {
	vi.useRealTimers()
	client.clear()
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

it('invalidates the boards and lists of the changed project', async () => {
	client.setQueryData(kanbanKeys.board(1, 4), {buckets: [], pages: {}})
	client.setQueryData(kanbanKeys.board(2, 8), {buckets: [], pages: {}})
	client.setQueryData(taskKeys.list({project: 1}), {items: []})
	client.setQueryData(taskKeys.list({project: 2}), {items: []})
	client.setQueryData(taskKeys.allList({project: 1}), {items: []})
	client.setQueryData(taskKeys.list({}), {items: []})
	mountUpdates()

	emit({project_id: 1, project_ids: [1], task_id: 0})

	await vi.waitFor(() => expect(isInvalidated(kanbanKeys.board(1, 4))).toBe(true))
	expect(isInvalidated(kanbanKeys.board(2, 8))).toBe(false)
	expect(isInvalidated(taskKeys.list({project: 1}))).toBe(true)
	expect(isInvalidated(taskKeys.allList({project: 1}))).toBe(true)
	expect(isInvalidated(taskKeys.list({project: 2}))).toBe(false)
	// A list without a project mixes tasks of every project.
	expect(isInvalidated(taskKeys.list({}))).toBe(true)
})

it('invalidates the ancestor projects of a sub-project change', async () => {
	client.setQueryData(kanbanKeys.board(3, 12), {buckets: [], pages: {}})
	client.setQueryData(kanbanKeys.board(7, 28), {buckets: [], pages: {}})
	mountUpdates()

	emit({project_id: 40, project_ids: [40, 3], task_id: 0})

	await vi.waitFor(() => expect(isInvalidated(kanbanKeys.board(3, 12))).toBe(true))
	expect(isInvalidated(kanbanKeys.board(7, 28))).toBe(false)
})

it('merges consecutive events into a single invalidation pass', async () => {
	vi.useFakeTimers()
	client.setQueryData(kanbanKeys.board(1, 4), {buckets: [], pages: {}})
	client.setQueryData(kanbanKeys.board(2, 8), {buckets: [], pages: {}})
	mountUpdates()

	emit({project_ids: [1]})
	emit({project_ids: [2]})
	vi.advanceTimersByTime(299)

	expect(isInvalidated(kanbanKeys.board(1, 4))).toBe(false)
	expect(isInvalidated(kanbanKeys.board(2, 8))).toBe(false)

	vi.advanceTimersByTime(1)

	expect(isInvalidated(kanbanKeys.board(1, 4))).toBe(true)
	expect(isInvalidated(kanbanKeys.board(2, 8))).toBe(true)
})

it('invalidates the task detail named by the event', async () => {
	client.setQueryData(taskKeys.detail(42), {id: 42})
	client.setQueryData(taskKeys.detail(43), {id: 43})
	mountUpdates()

	emit({project_ids: [1], task_id: 42})

	await vi.waitFor(() => expect(isInvalidated(taskKeys.detail(42))).toBe(true))
	expect(isInvalidated(taskKeys.detail(43))).toBe(false)
})

it('refetches the active board and only marks the inactive one stale', async () => {
	const activeKey = kanbanKeys.board(1, 4)
	const inactiveKey = kanbanKeys.board(2, 8)
	client.setQueryData(inactiveKey, {buckets: [], pages: {}})
	const fetchBoard = vi.fn().mockResolvedValue({buckets: [], pages: {}})
	const unsubscribe = new QueryObserver(client, {queryKey: activeKey, queryFn: fetchBoard}).subscribe(() => {})
	await vi.waitFor(() => expect(fetchBoard).toHaveBeenCalledTimes(1))
	mountUpdates()

	emit({project_ids: [1, 2]})

	await vi.waitFor(() => expect(fetchBoard).toHaveBeenCalledTimes(2))
	expect(isInvalidated(inactiveKey)).toBe(true)
	unsubscribe()
})

it('unsubscribes and drops a pending flush when the scope is disposed', async () => {
	vi.useFakeTimers()
	client.setQueryData(taskKeys.list({project: 1}), {items: []})
	const wrapper = mountUpdates()

	expect(ws.handlers.has('project.changed')).toBe(true)
	emit({project_ids: [1]})
	wrapper.unmount()

	expect(ws.unsubscribed).toEqual(['project.changed'])
	vi.advanceTimersByTime(300)
	await flushPromises()
	expect(isInvalidated(taskKeys.list({project: 1}))).toBe(false)
})

it('ignores events without projects or tasks', async () => {
	vi.useFakeTimers()
	client.setQueryData(kanbanKeys.board(1, 4), {buckets: [], pages: {}})
	mountUpdates()

	emit(null)
	emit({project_id: 0, project_ids: [], task_id: 0})
	vi.advanceTimersByTime(300)

	expect(isInvalidated(kanbanKeys.board(1, 4))).toBe(false)
})
