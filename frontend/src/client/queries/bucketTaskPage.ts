import {projectViewTasksList} from '@/client/generated'
import {BUCKET_TASK_PARAMS} from './kanban'
import type {TaskFilterParams} from './tasks'

export type BucketTaskPageInput = {
	project: number,
	view: number,
	params: TaskFilterParams,
	bucket: number,
	page: number,
}

// 翻页请求的拼法只有这一份：滚动加载和实时刷新必须请求同一页，否则两边会去重掉对方的卡片。
export async function fetchBucketTaskPage({project, view, params, bucket, page}: BucketTaskPageInput) {
	return (await projectViewTasksList({
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
	})).data
}
