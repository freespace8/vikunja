import type {QueryClient, QueryKey} from '@tanstack/vue-query'
import {projectViewBucketsTasksList, projectViewTasksList} from '@/client/generated'
import {
	BUCKET_TASK_PARAMS,
	kanbanKeys,
	normalizeBucket,
	type BoardData,
	type BucketResponse,
} from './kanban'
import {normalizeTask, type TaskFilterParams, type TaskResponse} from './tasks'

// 看板的每一列各自分页，请求第一页会把已经滚出来的页丢掉：这里按缓存里的 pages
// 把每列已加载的页都取回来再合并，行数不会缩回一页。
async function fetchBucketPage(
	project: number,
	view: number,
	params: TaskFilterParams,
	bucket: number,
	page: number,
): Promise<TaskResponse[]> {
	const {data} = await projectViewTasksList({
		path: {
			project,
			view,
		},
		query: {
			...params,
			...BUCKET_TASK_PARAMS,
			page,
			sort_by: ['position'],
			order_by: ['asc'],
			filter: `${params.filter ? `(${params.filter}) && ` : ''}bucket_id = ${bucket}`,
		},
	})
	return (data.items ?? []).map(normalizeTask)
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

export async function refetchBoardKeepingPages(client: QueryClient, key: QueryKey): Promise<void> {
	const project = kanbanKeys.projectOf(key)
	const view = kanbanKeys.viewOf(key)
	if (typeof project !== 'number' || typeof view !== 'number') return
	const params = kanbanKeys.paramsOf(key) ?? {}
	const loadedPages = client.getQueryData<BoardData>(key)?.pages ?? {}

	const {data} = await projectViewBucketsTasksList({
		path: {
			project,
			view,
		},
		query: {...params, ...BUCKET_TASK_PARAMS},
	})
	const pages: Record<number, number> = {}
	const buckets = await Promise.all((data.items ?? []).map(normalizeBucket).map(async bucket => {
		const lastPage = Math.max(1, loadedPages[bucket.id] ?? 1)
		if (lastPage === 1) {
			pages[bucket.id] = 1
			return bucket
		}
		const extra = await Promise.allSettled(Array.from(
			{length: lastPage - 1},
			(_, index) => fetchBucketPage(project, view, params, bucket.id, index + 2),
		))
		// 只统计成功的页；失败的那页下次滚动会重新请求，重复的任务在合并时去重。
		const fetched = extra
			.filter(result => result.status === 'fulfilled')
			.map(result => result.value)
		pages[bucket.id] = fetched.length + 1
		return mergePages(bucket, fetched)
	}))

	client.setQueryData<BoardData>(key, {
		buckets,
		pages,
	})
}

async function refetchOrInvalidate(client: QueryClient, key: QueryKey) {
	try {
		await refetchBoardKeepingPages(client, key)
	} catch {
		// 后台刷新失败不该弹错，先让它过期，下次打开看板时会重新请求。
		void client.invalidateQueries({
			queryKey: key,
			exact: true,
			refetchType: 'none',
		})
	}
}

// `predicate` 收到看板的 query key。没有观察者的看板只标记过期，下次打开时再取。
export async function refreshBoards(client: QueryClient, predicate?: (key: QueryKey) => boolean) {
	const queries = client.getQueryCache().findAll({
		queryKey: kanbanKeys.all,
		predicate: query => predicate?.(query.queryKey) ?? true,
	})

	await Promise.all(queries.map(query => query.getObserversCount() > 0
		? refetchOrInvalidate(client, query.queryKey)
		: client.invalidateQueries({
			queryKey: query.queryKey,
			exact: true,
			refetchType: 'none',
		})))
}
