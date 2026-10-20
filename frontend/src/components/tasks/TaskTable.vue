<template>
	<div class="has-horizontal-overflow">
		<table class="table has-actions is-hoverable is-fullwidth mbe-0">
			<thead>
				<tr>
					<th
						v-if="columns.index"
						:aria-sort="ariaSort('index')"
					>
						#
						<Sort
							:order="sortBy.index"
							label="#"
							@click="sort('index', $event)"
						/>
					</th>
					<th
						v-if="columns.done"
						:aria-sort="ariaSort('done')"
					>
						{{ $t('task.attributes.done') }}
						<Sort
							:order="sortBy.done"
							:label="$t('task.attributes.done')"
							@click="sort('done', $event)"
						/>
					</th>
					<th v-if="columns.project">
						{{ $t('task.attributes.project') }}
					</th>
					<th
						v-if="columns.title"
						:aria-sort="ariaSort('title')"
					>
						{{ $t('task.attributes.title') }}
						<Sort
							:order="sortBy.title"
							:label="$t('task.attributes.title')"
							@click="sort('title', $event)"
						/>
					</th>
					<th
						v-if="columns.priority"
						:aria-sort="ariaSort('priority')"
					>
						{{ $t('task.attributes.priority') }}
						<Sort
							:order="sortBy.priority"
							:label="$t('task.attributes.priority')"
							@click="sort('priority', $event)"
						/>
					</th>
					<th v-if="columns.labels">
						{{ $t('task.attributes.labels') }}
					</th>
					<th v-if="columns.assignees">
						{{ $t('task.attributes.assignees') }}
					</th>
					<th v-if="columns.bucket">
						{{ $t('task.attributes.bucket') }}
					</th>
					<th
						v-if="columns.dueDate"
						:aria-sort="ariaSort('due_date')"
					>
						{{ $t('task.attributes.dueDate') }}
						<Sort
							:order="sortBy.due_date"
							:label="$t('task.attributes.dueDate')"
							@click="sort('due_date', $event)"
						/>
					</th>
					<th v-if="columns.commentCount">
						{{ $t('task.attributes.commentCount') }}
					</th>
					<th
						v-if="columns.startDate"
						:aria-sort="ariaSort('start_date')"
					>
						{{ $t('task.attributes.startDate') }}
						<Sort
							:order="sortBy.start_date"
							:label="$t('task.attributes.startDate')"
							@click="sort('start_date', $event)"
						/>
					</th>
					<th
						v-if="columns.endDate"
						:aria-sort="ariaSort('end_date')"
					>
						{{ $t('task.attributes.endDate') }}
						<Sort
							:order="sortBy.end_date"
							:label="$t('task.attributes.endDate')"
							@click="sort('end_date', $event)"
						/>
					</th>
					<th
						v-if="columns.percentDone"
						:aria-sort="ariaSort('percent_done')"
					>
						{{ $t('task.attributes.percentDone') }}
						<Sort
							:order="sortBy.percent_done"
							:label="$t('task.attributes.percentDone')"
							@click="sort('percent_done', $event)"
						/>
					</th>
					<th
						v-if="columns.doneAt"
						:aria-sort="ariaSort('done_at')"
					>
						{{ $t('task.attributes.doneAt') }}
						<Sort
							:order="sortBy.done_at"
							:label="$t('task.attributes.doneAt')"
							@click="sort('done_at', $event)"
						/>
					</th>
					<th
						v-if="columns.created"
						:aria-sort="ariaSort('created')"
					>
						{{ $t('task.attributes.created') }}
						<Sort
							:order="sortBy.created"
							:label="$t('task.attributes.created')"
							@click="sort('created', $event)"
						/>
					</th>
					<th
						v-if="columns.updated"
						:aria-sort="ariaSort('updated')"
					>
						{{ $t('task.attributes.updated') }}
						<Sort
							:order="sortBy.updated"
							:label="$t('task.attributes.updated')"
							@click="sort('updated', $event)"
						/>
					</th>
					<th v-if="columns.createdBy">
						{{ $t('task.attributes.createdBy') }}
					</th>
				</tr>
			</thead>
			<tbody>
				<tr
					v-for="t in tasks"
					:key="t.id"
				>
					<td v-if="columns.index">
						<RouterLink :to="taskDetailRoutes[t.id]">
							{{ getTaskIdentifier(t) }}
						</RouterLink>
					</td>
					<td v-if="columns.done">
						<Done
							:is-done="t.done"
							variant="small"
						/>
					</td>
					<td v-if="columns.project">
						<RouterLink
							v-if="projectList.projects[t.project_id]"
							:to="{ name: 'project.index', params: { projectId: t.project_id } }"
						>
							{{ projectList.projects[t.project_id].title }}
						</RouterLink>
					</td>
					<td v-if="columns.title">
						<TaskGlanceTooltip :task="t">
							<RouterLink :to="taskDetailRoutes[t.id]">
								{{ t.title }}
							</RouterLink>
						</TaskGlanceTooltip>
					</td>
					<td v-if="columns.priority">
						<PriorityLabel
							:priority="t.priority"
							:show-all="true"
						/>
					</td>
					<td v-if="columns.labels">
						<Labels :labels="t.labels" />
					</td>
					<td v-if="columns.assignees">
						<AssigneeList
							v-if="t.assignees.length > 0"
							:assignees="t.assignees"
							:avatar-size="28"
							class="mis-1"
							:inline="true"
						/>
					</td>
					<td v-if="columns.bucket">
						{{ getTaskBucket(t, projectList.projects[t.project_id]?.views)?.title }}
					</td>
					<DateTableCell
						v-if="columns.dueDate"
						:date="t.due_date"
					/>
					<td v-if="columns.commentCount">
						<CommentCount :task="t" />
					</td>
					<DateTableCell
						v-if="columns.startDate"
						:date="t.start_date"
					/>
					<DateTableCell
						v-if="columns.endDate"
						:date="t.end_date"
					/>
					<td v-if="columns.percentDone">
						{{ t.percent_done * 100 }}%
					</td>
					<DateTableCell
						v-if="columns.doneAt"
						:date="t.done_at"
					/>
					<DateTableCell
						v-if="columns.created"
						:date="t.created"
					/>
					<DateTableCell
						v-if="columns.updated"
						:date="t.updated"
					/>
					<td v-if="columns.createdBy">
						<User
							:avatar-size="27"
							:show-username="false"
							:user="t.created_by ?? {}"
						/>
					</td>
				</tr>
			</tbody>
		</table>
	</div>
</template>

<script setup lang="ts">
import {computed} from 'vue'

import Done from '@/components/misc/Done.vue'
import User from '@/components/misc/User.vue'
import PriorityLabel from '@/components/tasks/partials/PriorityLabel.vue'
import Labels from '@/components/tasks/partials/Labels.vue'
import TaskGlanceTooltip from '@/components/tasks/partials/TaskGlanceTooltip.vue'
import DateTableCell from '@/components/tasks/partials/DateTableCell.vue'
import CommentCount from '@/components/tasks/partials/CommentCount.vue'
import Sort from '@/components/tasks/partials/Sort.vue'
import AssigneeList from '@/components/tasks/partials/AssigneeList.vue'
import type {TaskTableColumns} from '@/components/tasks/taskTableColumns'

import type {SortBy} from '@/composables/useTaskList'
import {useTableSort} from '@/composables/useTableSort'
import {useProjects} from '@/composables/useProjects'
import type {TaskResponse} from '@/client/queries/tasks'
import {getTaskBucket, getTaskIdentifier} from '@/helpers/task'

const props = defineProps<{
	tasks: TaskResponse[],
	columns: TaskTableColumns,
	sortBy: SortBy,
}>()

const emit = defineEmits<{
	'update:sortBy': [value: SortBy],
	'sorted': [],
}>()

const projectList = useProjects()

const sortByModel = computed({
	get: () => props.sortBy,
	set: value => emit('update:sortBy', value),
})
// The parent stores the new sort before `sorted` fires, so it can read it there.
const {sort, ariaSort} = useTableSort<keyof SortBy>(sortByModel, () => emit('sorted'))

// TODO: re-enable opening task detail in modal
// const router = useRouter()
const taskDetailRoutes = computed(() => Object.fromEntries(
	props.tasks.map(({id}) => ([
		id,
		{
			name: 'task.detail',
			params: {id},
			// state: { backdropView: router.currentRoute.value.fullPath },
		},
	])),
))
</script>

<style lang="scss" scoped>
.table {
	background: transparent;
	overflow-x: auto;
	overflow-y: hidden;

	th {
		white-space: nowrap;
	}

	.user {
		margin: 0;
	}
}
</style>
