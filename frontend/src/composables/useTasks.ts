import {computed, toValue, type MaybeRefOrGetter} from 'vue'
import {useQuery, type Query} from '@tanstack/vue-query'
import {taskKeys, tasksQuery, type PaginatedTaskResponse, type TaskScope} from '@/client/queries/tasks'

type TaskListQuery = Query<
	PaginatedTaskResponse,
	Error,
	PaginatedTaskResponse,
	ReturnType<typeof taskKeys.list>
>

export type UseTasksOptions = {
	enabled?: MaybeRefOrGetter<boolean>
	page?: MaybeRefOrGetter<number>
	placeholderData?: (
		previousData: PaginatedTaskResponse | undefined,
		previousQuery: TaskListQuery | undefined,
	) => PaginatedTaskResponse | undefined
}

export function useTasks(
	scope: MaybeRefOrGetter<TaskScope>,
	{enabled = true, page = 1, placeholderData}: UseTasksOptions = {},
) {
	const query = useQuery(computed(() => ({
		...tasksQuery(toValue(scope), toValue(page)),
		enabled: toValue(enabled),
		placeholderData,
	})))
	const tasks = computed(() => query.data.value?.items ?? [])
	const total = computed(() => query.data.value?.total ?? 0)
	const totalPages = computed(() => query.data.value?.total_pages ?? 0)
	return {...query, tasks, total, totalPages}
}
