import type {BucketResponse} from '@/client/queries/kanban'
import type {TaskResponse} from '@/client/queries/tasks'

// The server's order of a column on a board that includes sub-projects: the board's own tasks by position,
// then the sub-project tasks, which have none in this view, by id. Local cache patches would scramble it.
export function sortAggregateBucketTasks(tasks: readonly TaskResponse[], boardProjectId: number): TaskResponse[] {
	const isOwn = (task: TaskResponse) => task.project_id === boardProjectId
	return [...tasks].sort((a, b) => {
		if (isOwn(a) !== isOwn(b)) return isOwn(a) ? -1 : 1
		return (isOwn(a) ? a.position - b.position : 0) || a.id - b.id
	})
}

export function sortAggregateBuckets(buckets: readonly BucketResponse[], boardProjectId: number): BucketResponse[] {
	return buckets.map(bucket => ({...bucket, tasks: sortAggregateBucketTasks(bucket.tasks, boardProjectId)}))
}
