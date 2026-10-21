import type {QueryClient, QueryKey} from '@tanstack/vue-query'
import {kanbanKeys} from './kanban'
import {taskKeys} from './tasks'

// A parent project's board that also holds the cards of its sub-projects.
// Its membership is decided by the server, so local cache patches must not drop cards of other projects from it.
export const isAggregateBoardKey = (key: QueryKey) => kanbanKeys.paramsOf(key)?.include_subprojects === true

// Refetching resets the loaded pages of every bucket, so this is for rare changes only.
// `exceptProject`: the boards of that project show the task as their own card and are patched locally.
export function refetchAggregateBoards(client: QueryClient, exceptProject?: number) {
	return client.invalidateQueries({
		queryKey: kanbanKeys.all,
		refetchType: 'active',
		predicate: query => isAggregateBoardKey(query.queryKey)
			&& (exceptProject === undefined || kanbanKeys.projectOf(query.queryKey) !== exceptProject),
	})
}

// A task that sits on its own project's board shows on the parent's boards under a mapping only the server knows,
// so they are read again instead of patched. Moved through a parent board, the detail's buckets keep the stale
// entry of the task's own board.
export function syncAggregateBoardsAfterMove(
	client: QueryClient,
	{boardProject, taskProject, taskId}: {boardProject: number, taskProject: number | undefined, taskId: number | undefined},
) {
	if (taskProject === undefined || taskId === undefined) return
	void refetchAggregateBoards(client, taskProject)
	if (taskProject !== boardProject) {
		void client.invalidateQueries({queryKey: [...taskKeys.details, taskId], refetchType: 'active'})
	}
}
