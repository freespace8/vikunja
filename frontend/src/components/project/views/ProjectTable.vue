<template>
	<ProjectWrapper
		class="project-table"
		:is-loading-project="isLoadingProject"
		:project-id="projectId"
		:view-id
	>
		<template #header>
			<div class="filter-container">
				<TaskTableColumnPicker v-model="activeColumns" />
				<FilterPopup
					v-if="!isSavedFilterProject({id: projectId})"
					v-model="params"
					:view-id="viewId"
					:project-id="projectId"
					@update:modelValue="taskList.loadTasks()"
				/>
			</div>
		</template>

		<template #default>
			<div
				:class="{'is-loading': loading}"
				class="loader-container"
			>
				<Card
					:padding="false"
					:has-content="false"
				>
					<TaskTable
						v-model:sort-by="sortBy"
						:tasks="tasks"
						:columns="activeColumns"
						@sorted="setActiveColumnsSortParam"
					/>

					<Pagination
						:total-pages="totalPages"
						:current-page="currentPage"
					/>
				</Card>
			</div>
		</template>
	</ProjectWrapper>
</template>

<script setup lang="ts">
import {computed, type Ref, watch} from 'vue'

import {useStorage} from '@vueuse/core'

import ProjectWrapper from '@/components/project/ProjectWrapper.vue'
import TaskTable from '@/components/tasks/TaskTable.vue'
import TaskTableColumnPicker from '@/components/tasks/TaskTableColumnPicker.vue'
import FilterPopup from '@/components/project/partials/FilterPopup.vue'
import Pagination from '@/components/misc/Pagination.vue'

import type {SortBy} from '@/composables/useTaskList'
import {useTaskList} from '@/composables/useTaskList'
import type {TaskResponse} from '@/client/queries/tasks'
import {useDelayedLoading} from '@/composables/useDelayedLoading'
import {ACTIVE_COLUMNS_DEFAULT, sortByActiveColumns, type TaskTableColumns} from '@/components/tasks/taskTableColumns'
import {isSavedFilterProject} from '@/client/queries/projects'
import {useIncludeSubprojects} from '@/composables/useIncludeSubprojects'

const props = defineProps<{
	isLoadingProject: boolean,
	projectId: number,
	viewId: number,
}>()

const SORT_BY_DEFAULT: SortBy = {
	index: 'desc',
}

const sortBy = useStorage<SortBy>('tableViewSortBy', {...SORT_BY_DEFAULT})

const includeSubprojects = useIncludeSubprojects(() => props.projectId)
// Parent projects keep their own column choice so showing Project by default there doesn't change other tables.
const plainColumns = useStorage('tableViewColumns', {...ACTIVE_COLUMNS_DEFAULT})
const aggregateColumns = useStorage('tableViewColumnsAggregate', {...plainColumns.value, project: true})
const activeColumns = computed<TaskTableColumns>({
	get: () => includeSubprojects.value ? aggregateColumns.value : plainColumns.value,
	set: columns => {
		if (includeSubprojects.value) {
			aggregateColumns.value = columns
		} else {
			plainColumns.value = columns
		}
	},
})

const taskList = useTaskList(
	() => props.projectId,
	() => props.viewId,
	sortBy.value,
	() => ['comment_count', 'is_unread'],
	() => includeSubprojects.value,
)

const {
	params,
	totalPages,
	currentPage,
	sortByParam,
} = taskList
const loading = useDelayedLoading(taskList.loading)
const tasks: Ref<TaskResponse[]> = taskList.tasks

// Not the switching computed: gaining sub-projects must not rewrite the sort and page of the current url.
watch(
	[plainColumns, aggregateColumns],
	() => setActiveColumnsSortParam(),
	{deep: true},
)

function setActiveColumnsSortParam() {
	sortByParam.value = sortByActiveColumns(sortBy.value, activeColumns.value)
}
</script>

<style lang="scss" scoped>
.link-share-view .card {
	border: none;
	box-shadow: none;
}
</style>
