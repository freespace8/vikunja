import {onScopeDispose} from 'vue'
import {useQueryClient} from '@tanstack/vue-query'
import {kanbanKeys} from '@/client/queries/kanban'
import {taskKeys} from '@/client/queries/tasks'
import {useWebSocket} from './useWebSocket'

// One change fires several events (task, position, relation); wait for the burst to settle before refetching.
const FLUSH_DELAY_MS = 300

type ProjectChangedData = {
	project_id?: number
	project_ids?: number[]
	task_id?: number
}

// Keeps boards, lists and task details in sync with changes made elsewhere (other tabs, other users).
// The server only sends ids, so this marks the affected queries stale and lets the ones with
// observers refetch through the regular API.
export function useProjectLiveUpdates() {
	const socket = useWebSocket()
	const client = useQueryClient()

	let timer: ReturnType<typeof setTimeout> | undefined
	let projectIDs = new Set<number>()
	let taskIDs = new Set<number>()

	const flush = () => {
		timer = undefined
		const projects = projectIDs
		const tasks = taskIDs
		projectIDs = new Set()
		taskIDs = new Set()

		if (projects.size > 0) {
			client.invalidateQueries({
				queryKey: kanbanKeys.all,
				refetchType: 'active',
				predicate: query => {
					const project = kanbanKeys.projectOf(query.queryKey)
					return typeof project === 'number' && projects.has(project)
				},
			})

			for (const queryKey of [taskKeys.lists, taskKeys.allLists]) {
				client.invalidateQueries({
					queryKey,
					refetchType: 'active',
					predicate: query => {
						const project = taskKeys.projectOf(query.queryKey)
						// Lists without a project (overview, upcoming) aggregate tasks of every project.
						return project === null || (typeof project === 'number' && projects.has(project))
					},
				})
			}
		}

		for (const taskID of tasks) {
			client.invalidateQueries({queryKey: [...taskKeys.details, taskID], refetchType: 'active'})
		}
	}

	const unsubscribe = socket.subscribe('project.changed', message => {
		const data = message.data as ProjectChangedData | null | undefined
		if (!data || typeof data !== 'object') return

		for (const projectID of data.project_ids ?? []) {
			if (typeof projectID === 'number' && projectID > 0) projectIDs.add(projectID)
		}
		if (typeof data.task_id === 'number' && data.task_id > 0) taskIDs.add(data.task_id)
		if (projectIDs.size === 0 && taskIDs.size === 0) return

		clearTimeout(timer)
		timer = setTimeout(flush, FLUSH_DELAY_MS)
	})

	onScopeDispose(() => {
		unsubscribe()
		if (timer !== undefined) clearTimeout(timer)
		timer = undefined
	})
}
