import type {QueryClient, QueryKey} from '@tanstack/vue-query'
import {kanbanKeys} from './kanban'

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
