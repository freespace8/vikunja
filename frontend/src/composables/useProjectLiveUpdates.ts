import {
	onScopeDispose,
	watch,
} from 'vue'
import {
	hashKey,
	useQueryClient,
	type QueryKey,
} from '@tanstack/vue-query'
import {bucketKeys, kanbanKeys} from '@/client/queries/kanban'
import {projectKeys} from '@/client/queries/projects'
import {taskKeys} from '@/client/queries/tasks'
import {taskQueryKeys} from '@/client/queries/taskCache'
import {refreshBoards} from '@/client/queries/boardRefetch'
import {useWebSocket} from './useWebSocket'

// 一次改动会连发几个事件（任务、位置、关系），等这一串事件稳定后再刷新。
const FLUSH_DELAY_MS = 300

type ProjectChangedData = {
	project_ids?: number[]
	task_id?: number
	// 服务端给的分类：layout 可能移动卡片，content 只改任务内容，project 改的是项目/视图/分桶本身。
	kind?: 'layout' | 'content' | 'project'
}

// 让看板、列表和任务详情跟着别处（其他标签页、其他用户）的改动更新。
// 服务端只发 ID，所以这里让受影响的缓存失效，有观察者的缓存再通过普通 API 重新读取。
export function useProjectLiveUpdates() {
	const socket = useWebSocket()
	const client = useQueryClient()

	let timer: ReturnType<typeof setTimeout> | undefined
	let projectIDs = new Set<number>()
	let taskIDs = new Set<number>()
	let structureChanged = false

	// 父项目、收藏和保存的筛选器的看板与列表都会显示受影响项目的任务。
	// 筛选器是负数 ID 的伪项目，null 是没有项目的聚合列表，两者一律视为受影响。
	const affected = (project: number | null | undefined, projects: Set<number>) =>
		project === null || (typeof project === 'number' && (project < 0 || projects.has(project)))

	const invalidateLists = (projects: Set<number>) => {
		const predicate = (query: {queryKey: QueryKey}) => affected(taskKeys.projectOf(query.queryKey), projects)
		return Promise.all([
			client.invalidateQueries({
				queryKey: taskKeys.lists,
				refetchType: 'active',
				predicate,
			}),
			client.invalidateQueries({
				queryKey: taskKeys.allLists,
				refetchType: 'active',
				predicate,
			}),
		])
	}

	// 任务换项目时后端只知道新项目，旧项目的看板靠缓存里是否还有这张卡来补偿。
	const refreshTaskCollections = async (id: number) => {
		const keys = taskQueryKeys(client, id)
		const hashes = new Set(keys.map(hashKey))
		await Promise.all([
			refreshBoards(client, key => hashes.has(hashKey(key))),
			...keys
				.filter(key => key[0] !== kanbanKeys.all[0])
				.map(key => client.invalidateQueries({
					queryKey: key,
					exact: true,
					refetchType: 'active',
				})),
		])
	}

	const flush = async () => {
		timer = undefined
		const projects = projectIDs
		const tasks = taskIDs
		const structure = structureChanged
		projectIDs = new Set()
		taskIDs = new Set()
		structureChanged = false

		if (projects.size > 0) {
			await Promise.all([
				refreshBoards(client, key => affected(kanbanKeys.projectOf(key), projects)),
				invalidateLists(projects),
			])
		}

		if (structure) {
			// 视图和分桶存在项目缓存里，列选择器用的是 bucketKeys。
			await Promise.all([
				client.invalidateQueries({
					queryKey: projectKeys.all,
					refetchType: 'active',
				}),
				client.invalidateQueries({
					queryKey: bucketKeys.all,
					refetchType: 'active',
				}),
			])
		}

		for (const taskID of tasks) {
			await refreshTaskCollections(taskID)
		}
	}

	const unsubscribe = socket.subscribe('project.changed', message => {
		const data = message.data as ProjectChangedData | null | undefined
		if (!data || typeof data !== 'object') return

		const kind = data.kind ?? 'layout'
		if (kind !== 'content') {
			for (const projectID of data.project_ids ?? []) {
				if (typeof projectID === 'number' && projectID > 0) projectIDs.add(projectID)
			}
		}
		if (kind === 'project') structureChanged = true
		if (typeof data.task_id === 'number' && data.task_id > 0) taskIDs.add(data.task_id)
		if (projectIDs.size === 0 && taskIDs.size === 0 && !structureChanged) return

		clearTimeout(timer)
		timer = setTimeout(() => void flush(), FLUSH_DELAY_MS)
	})

	// 断线期间的推送会丢，重连后把当前显示的看板和列表重新读一遍。
	let authenticatedBefore = socket.authenticated.value
	const stopWatchingAuth = watch(socket.authenticated, authenticated => {
		if (!authenticated) return
		if (authenticatedBefore || socket.mayHaveMissedEvents.value) {
			void Promise.all([
				refreshBoards(client),
				client.invalidateQueries({
					queryKey: taskKeys.lists,
					refetchType: 'active',
				}),
				client.invalidateQueries({
					queryKey: taskKeys.allLists,
					refetchType: 'active',
				}),
				client.invalidateQueries({
					queryKey: taskKeys.details,
					refetchType: 'active',
				}),
			])
		}
		authenticatedBefore = true
	})

	onScopeDispose(() => {
		unsubscribe()
		stopWatchingAuth()
		if (timer !== undefined) clearTimeout(timer)
		timer = undefined
	})
}
