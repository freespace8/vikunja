import {useMutation} from '@tanstack/vue-query'
import {bucketsCreate, bucketsUpdate, bucketsDelete, type Bucket} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {bucketKeys, kanbanKeys, kanbanMutationKeys, normalizeBucket, type BoardData} from './kanban'
import {fetchBucketTaskPage, type BucketTaskPageInput} from './bucketTaskPage'
import {normalizeTask} from './tasks'
import {invalidateTaskMembership} from './taskCache'
import {projectKeys} from './projects'

type BucketDraft = Required<Pick<Bucket, 'title'>>
// The update is a PUT, so a partial body would reset limit and position.
type BucketUpdate = Required<Pick<Bucket, 'id' | 'title' | 'limit' | 'position'>>

// 看板结构的 mutation 也会写 BoardData，实时刷新据此避让，避免覆盖乐观更新。
function kanbanMutation<TOptions extends object>(options: TOptions): TOptions & {mutationKey: readonly unknown[]} {
	return {...options, mutationKey: kanbanMutationKeys.all}
}

export function createBucketMutationOptions() {
	return kanbanMutation(contextMutationOptions({
		mutationFn: async ({project, view, bucket}: {project: number, view: number, bucket: BucketDraft}) =>
			(await bucketsCreate({path: {project, view}, body: {title: bucket.title}})).data,
		onSuccess: (bucket, {project, view}, client) => {
			const created = normalizeBucket(bucket)
			client.setQueriesData<BoardData>(
				{queryKey: kanbanKeys.view(project, view)},
				current => current ? {
					...current,
					buckets: [...current.buckets, created],
					pages: {...current.pages, [created.id]: 1},
				} : current,
			)
		},
		onSettled: ({project, view}, client) => Promise.all([
			invalidateTaskMembership(client),
			client.invalidateQueries({queryKey: bucketKeys.list(project, view)}),
		]),
	}))
}

export function updateBucketMutationOptions(successMessage?: string) {
	return kanbanMutation(contextMutationOptions({
		mutationFn: async ({project, view, bucket}: {project: number, view: number, bucket: BucketUpdate}) =>
			(await bucketsUpdate({
				path: {project, view, bucket: bucket.id},
				body: {title: bucket.title, limit: bucket.limit, position: bucket.position},
			})).data,
		optimistic: {
			queryKeys: ({project, view}) => [kanbanKeys.view(project, view)],
			update: ({project, view, bucket}, client) => client.setQueriesData<BoardData>(
				{queryKey: kanbanKeys.view(project, view)},
				current => current ? {
					...current,
					buckets: current.buckets.map(item => item.id === bucket.id ? {...item, ...bucket} : item),
				} : current,
			),
		},
		onSuccess: (bucket, {project, view}, client) => client.setQueriesData<BoardData>(
			{queryKey: kanbanKeys.view(project, view)},
			current => current ? {
				...current,
				buckets: current.buckets
					.map(item => item.id === bucket.id
						? normalizeBucket({...item, ...bucket, tasks: item.tasks, count: item.count})
						: item)
					.sort((a, b) => a.position - b.position),
			} : current,
		),
		onSettled: ({project, view}, client) => Promise.all([
			invalidateTaskMembership(client),
			client.invalidateQueries({queryKey: bucketKeys.list(project, view)}),
		]),
		successMessage: () => successMessage,
	}))
}

export function deleteBucketMutationOptions(successMessage?: string) {
	return kanbanMutation(contextMutationOptions({
		mutationFn: async ({project, view, bucket}: {project: number, view: number, bucket: number}) => {
			await bucketsDelete({path: {project, view, bucket}})
		},
		onSuccess: (_data, {project, view, bucket}, client) => client.setQueriesData<BoardData>(
			{queryKey: kanbanKeys.view(project, view)},
			current => {
				if (!current) return current
				const {[bucket]: _page, ...pages} = current.pages
				return {...current, buckets: current.buckets.filter(item => item.id !== bucket), pages}
			},
		),
		onSettled: async ({project, view}, client) => {
			await Promise.all([
				client.invalidateQueries({queryKey: kanbanKeys.view(project, view)}),
				// The backend clears the view's default and done bucket ids.
				client.invalidateQueries({queryKey: projectKeys.all}),
				invalidateTaskMembership(client),
				client.invalidateQueries({queryKey: bucketKeys.list(project, view)}),
			])
		},
		successMessage: () => successMessage,
	}))
}

export function loadBucketPageMutationOptions() {
	return kanbanMutation(contextMutationOptions({
		mutationFn: async (input: BucketTaskPageInput) => fetchBucketTaskPage(input),
		onSuccess: (data, {project, view, params, bucket, page}, client) => client.setQueryData<BoardData>(
			kanbanKeys.board(project, view, params),
			current => {
				if (!current || (current.pages[bucket] ?? 1) !== page - 1) return current
				return {
					...current,
					buckets: current.buckets.map(item => item.id === bucket
						? {
							...item,
							tasks: [
								...item.tasks,
								...(data.items ?? [])
									.map(normalizeTask)
									.filter(task => !item.tasks.some(old => old.id === task.id)),
							],
						}
						: item),
					pages: {...current.pages, [bucket]: page},
				}
			},
		),
	}))
}

export function useCreateBucketMutation() {
	return useMutation(createBucketMutationOptions())
}
export function useUpdateBucketMutation(successMessage?: string) {
	return useMutation(updateBucketMutationOptions(successMessage))
}
export function useDeleteBucketMutation(successMessage?: string) {
	return useMutation(deleteBucketMutationOptions(successMessage))
}
export function useLoadBucketPageMutation() {
	return useMutation(loadBucketPageMutationOptions())
}
