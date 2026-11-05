import {
	hashKey,
	type QueryClient,
	type QueryKey,
} from '@tanstack/vue-query'
import {projectViewBucketsTasksList} from '@/client/generated'
import {
	BUCKET_TASK_PARAMS,
	kanbanKeys,
	kanbanMutationKeys,
	normalizeBucket,
	type BoardData,
	type BucketResponse,
} from './kanban'
import {fetchBucketTaskPage} from './bucketTaskPage'
import {normalizeTask, type TaskFilterParams, type TaskResponse} from './tasks'

type BoardRefresh = {
	rerun: boolean,
	running: Promise<void>,
}

// 同一块看板同一时刻只跑一次刷新；刷新期间再来的请求合并成结束后再刷一次。
const boardRefreshes = new Map<string, BoardRefresh>()
// 有看板 mutation 在跑时先不写回，等它结束后补一次，免得覆盖乐观更新。
const waitingForMutations = new Map<string, QueryKey>()
let stopWatchingMutations: (() => void) | undefined

// 只保留从 fromPage 开始连续成功的页：中间某页失败后停止，页号不会跳过失败的那页。
async function fetchBucketPages(
	project: number,
	view: number,
	params: TaskFilterParams,
	bucket: number,
	fromPage: number,
	lastPage: number,
): Promise<TaskResponse[][]> {
	if (lastPage < fromPage) return []

	const results = await Promise.allSettled(Array.from(
		{length: lastPage - fromPage + 1},
		(_, index) => fetchBucketTaskPage({
			project,
			view,
			params,
			bucket,
			page: fromPage + index,
		}),
	))
	const pages: TaskResponse[][] = []
	for (const result of results) {
		if (result.status !== 'fulfilled') break
		pages.push((result.value.items ?? []).map(normalizeTask))
	}
	return pages
}

function mergePages(bucket: BucketResponse, pages: TaskResponse[][]): BucketResponse {
	const tasks = [...bucket.tasks]
	for (const page of pages) {
		for (const task of page) {
			if (!tasks.some(existing => existing.id === task.id)) tasks.push(task)
		}
	}
	return {...bucket, tasks}
}

function kanbanMutationsRunning(client: QueryClient) {
	return client.isMutating({mutationKey: kanbanMutationKeys.all}) > 0
}

// mutation 结束后把等着的看板各补刷一次。
function waitForKanbanMutations(client: QueryClient, key: QueryKey) {
	waitingForMutations.set(hashKey(key), key)
	if (stopWatchingMutations) return

	stopWatchingMutations = client.getMutationCache().subscribe(() => {
		if (kanbanMutationsRunning(client)) return
		stopWatchingMutations?.()
		stopWatchingMutations = undefined
		const waiting = [...waitingForMutations.values()]
		waitingForMutations.clear()
		for (const boardKey of waiting) void refreshBoard(client, boardKey)
	})
}

async function refetchBoardKeepingPages(client: QueryClient, key: QueryKey): Promise<void> {
	const project = kanbanKeys.projectOf(key)
	const view = kanbanKeys.viewOf(key)
	if (typeof project !== 'number' || typeof view !== 'number') return
	const params = kanbanKeys.paramsOf(key) ?? {}

	await client.cancelQueries({
		queryKey: key,
		exact: true,
	})
	const {data} = await projectViewBucketsTasksList({
		path: {
			project,
			view,
		},
		query: {...params, ...BUCKET_TASK_PARAMS},
	})
	// 看板的每一列各自分页，第一页只带每列的头几张：按缓存里的 pages 把滚出来的页一起取回来。
	const base = (data.items ?? []).map(normalizeBucket)
	const buckets = new Map(base.map(bucket => [bucket.id, bucket]))
	const pages: Record<number, number> = Object.fromEntries(base.map(bucket => [bucket.id, 1]))
	// 拉取期间“加载下一页”可能先完成：写回前重新读缓存里的 pages，补齐比手上更新的页。
	const stalled = new Set<number>()
	for (;;) {
		const loaded = client.getQueryData<BoardData>(key)?.pages ?? {}
		const missing = base.filter(bucket => !stalled.has(bucket.id) && (loaded[bucket.id] ?? 1) > pages[bucket.id])
		if (missing.length === 0) break

		let progressed = false
		await Promise.all(missing.map(async bucket => {
			const fromPage = pages[bucket.id] + 1
			const lastPage = Math.max(fromPage, loaded[bucket.id] ?? 1)
			const fetched = await fetchBucketPages(project, view, params, bucket.id, fromPage, lastPage)
			if (fetched.length === 0) {
				stalled.add(bucket.id)
				return
			}
			progressed = true
			pages[bucket.id] = fromPage + fetched.length - 1
			buckets.set(bucket.id, mergePages(buckets.get(bucket.id) ?? bucket, fetched))
		}))
		if (!progressed) break
	}

	if (kanbanMutationsRunning(client)) {
		waitForKanbanMutations(client, key)
		return
	}

	client.setQueryData<BoardData>(key, {
		buckets: base.map(bucket => buckets.get(bucket.id) ?? bucket),
		pages,
	})
}

function refreshBoard(client: QueryClient, key: QueryKey): Promise<void> {
	const id = hashKey(key)
	const active = boardRefreshes.get(id)
	if (active) {
		active.rerun = true
		return active.running
	}

	const refresh: BoardRefresh = {
		rerun: false,
		running: Promise.resolve(),
	}
	boardRefreshes.set(id, refresh)
	refresh.running = (async () => {
		try {
			do {
				refresh.rerun = false
				await refetchBoardKeepingPages(client, key)
			} while (refresh.rerun && !kanbanMutationsRunning(client))
		} finally {
			boardRefreshes.delete(id)
		}
	})()
	return refresh.running
}

// `predicate` 收到看板的 query key。没有观察者的看板只标记过期，下次打开时再取。
export async function refreshBoards(client: QueryClient, predicate?: (key: QueryKey) => boolean) {
	const queries = client.getQueryCache().findAll({
		queryKey: kanbanKeys.all,
		predicate: query => predicate?.(query.queryKey) ?? true,
	})

	await Promise.all(queries.map(query => query.getObserversCount() > 0
		? refreshBoard(client, query.queryKey)
		: client.invalidateQueries({
			queryKey: query.queryKey,
			exact: true,
			refetchType: 'none',
		})))
}
