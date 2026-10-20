<template>
	<div
		v-cy="'showTasks'"
		class="is-max-width-desktop has-text-start"
	>
		<h2 class="mbe-2 title">
			{{ pageTitle }}
		</h2>
		<Message
			v-if="filteredLabels.length > 0"
			class="label-filter-info mbe-2"
		>
			<i18n-t
				keypath="task.show.filterByLabel"
				tag="span"
				class="filter-label-text"
			>
				<template #label>
					<XLabel
						v-for="label in filteredLabels"
						:key="label.id"
						:label="label"
					/>
				</template>
			</i18n-t>
			<BaseButton
				v-tooltip="$t('task.show.clearLabelFilter')"
				class="clear-filter-button"
				:aria-label="$t('task.show.clearLabelFilter')"
				@click="clearLabelFilter"
			>
				<Icon icon="times" />
			</BaseButton>
		</Message>
		<Message
			v-if="savedFilterIgnored"
			class="mbe-2"
		>
			{{ $t('task.show.savedFilterIgnored') }}
		</Message>
		<p
			v-if="!showAll"
			class="show-tasks-options"
		>
			<DatepickerWithRange
				:model-value="{dateFrom: dateFrom ?? null, dateTo: dateTo ?? null}"
				@update:modelValue="setDate"
			>
				<template #trigger="{toggle}">
					<XButton
						variant="primary"
						:shadow="false"
						class="mbe-2"
						@click.prevent.stop="toggle()"
					>
						{{ $t('task.show.select') }}
					</XButton>
				</template>
			</DatepickerWithRange>
			<FancyCheckbox
				:model-value="showNulls"
				class="mie-2"
				@update:modelValue="setShowNulls"
			>
				{{ $t('task.show.noDates') }}
			</FancyCheckbox>
			<FancyCheckbox
				:model-value="showOverdue"
				@update:modelValue="setShowOverdue"
			>
				{{ $t('task.show.overdue') }}
			</FancyCheckbox>
		</p>
		<div class="show-tasks-view-options mbe-2">
			<div
				class="view-mode-switch"
				role="group"
				:aria-label="$t('menu.views')"
			>
				<XButton
					v-for="mode in VIEW_MODES"
					:key="mode.value"
					:variant="viewMode === mode.value ? 'primary' : 'secondary'"
					:shadow="false"
					:aria-pressed="viewMode === mode.value"
					@click="viewMode = mode.value"
				>
					{{ $t(mode.label) }}
				</XButton>
			</div>
			<TaskTableColumnPicker
				v-if="isTableView"
				v-model="tableColumns"
			/>
		</div>
		<template v-if="!loading && (!tasks || tasks.length === 0) && showNothingToDo">
			<h3 class="has-text-centered mbs-6">
				{{ $t('task.show.noTasks') }}
			</h3>
			<LlamaCool class="llama-cool" />
		</template>

		<Card
			v-if="hasTasks"
			:padding="false"
			:class="{'has-overflow': !isTableView}"
			:has-content="false"
			:loading="loading"
		>
			<TaskTable
				v-if="isTableView"
				v-model:sort-by="tableSortBy"
				:tasks="tasks"
				:columns="tableColumns"
			/>
			<ul
				v-else
				class="p-2 tasks"
			>
				<li
					v-for="task in tasks"
					:key="task.id"
				>
					<SingleTaskInProject
						:show-project="true"
						:the-task="task"
						:can-mark-as-done="(projectList.projects[task.project_id]?.max_permission ?? 0) > PERMISSIONS.READ"
						@taskUpdated="updateTasks"
					/>
				</li>
			</ul>
		</Card>
		<div
			v-else
			:class="{ 'is-loading': showLoading}"
			class="spinner"
		/>
	</div>
</template>

<script setup lang="ts">
import {computed, ref, watch, watchEffect} from 'vue'
import {useRoute, useRouter} from 'vue-router'
import {useStorage} from '@vueuse/core'
import {hashKey, type QueryKey} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'

import {formatDate} from '@/helpers/time/formatDate'
import {setTitle} from '@/helpers/setTitle'

import BaseButton from '@/components/base/BaseButton.vue'
import Icon from '@/components/misc/Icon'
import Message from '@/components/misc/Message.vue'
import FancyCheckbox from '@/components/input/FancyCheckbox.vue'
import SingleTaskInProject from '@/components/tasks/partials/SingleTaskInProject.vue'
import TaskTable from '@/components/tasks/TaskTable.vue'
import TaskTableColumnPicker from '@/components/tasks/TaskTableColumnPicker.vue'
import {ACTIVE_COLUMNS_DEFAULT, sortByActiveColumns} from '@/components/tasks/taskTableColumns'
import DatepickerWithRange from '@/components/date/DatepickerWithRange.vue'
import XLabel from '@/components/tasks/partials/Label.vue'
import {DATE_RANGES} from '@/components/date/dateRanges'
import LlamaCool from '@/assets/llama-cool.svg?component'
import {useAuthStore} from '@/stores/auth'
import {useProjects} from '@/composables/useProjects'
import {useLabels} from '@/composables/useLabels'
import {taskKeys, type TaskFilterParams} from '@/client/queries/tasks'
import type {SortBy} from '@/composables/useTaskList'
import {useTasks} from '@/composables/useTasks'
import {useDelayedLoading} from '@/composables/useDelayedLoading'
import type {TaskScope} from '@/client/queries/tasks'
import {PERMISSIONS} from '@/constants/permissions'

const props = withDefaults(defineProps<{
	dateFrom?: Date | string,
	dateTo?: Date | string,
	showNulls?: boolean,
	showOverdue?: boolean,
	labelIds?: string[],
}>(), {
	showNulls: false,
	showOverdue: false,
	dateFrom: undefined,
	dateTo: undefined,
	labelIds: undefined,
})

const emit = defineEmits<{
	'tasksLoaded': [loaded: true],
	'clearLabelFilter': [],
}>()

const authStore = useAuthStore()
const projectList = useProjects()
const {getLabelById} = useLabels()

const route = useRoute()
const router = useRouter()
const {t} = useI18n({useScope: 'global'})

type ViewMode = 'list' | 'table'

const VIEW_MODES: {
	value: ViewMode,
	label: string,
}[] = [
	{
		value: 'list',
		label: 'project.list.title',
	},
	{
		value: 'table',
		label: 'project.table.title',
	},
]

const viewMode = useStorage<ViewMode>('overviewViewMode', 'list')
const isTableView = computed(() => viewMode.value === 'table')
// Own keys: the tasks of all projects are shown here, so the Project column is on by default.
// mergeDefaults: columns added later (bucket) take their default instead of staying hidden for stored settings.
const tableColumns = useStorage(
	'overviewTableColumns',
	{...ACTIVE_COLUMNS_DEFAULT, project: true},
	undefined,
	{mergeDefaults: true},
)
const tableSortBy = useStorage<SortBy>('overviewTableSortBy', {})

const taskScope = ref<TaskScope | null>(null)
// Only a new sort keeps the previous tasks, so the table and its sort buttons stay mounted while it reloads.
// A changed filter must not show the tasks of the previous one.
function withoutSort(key: QueryKey): QueryKey {
	const params = taskKeys.paramsOf(key)
	if (!params) {
		return key
	}
	const filterParams = {...params}
	delete filterParams.sort_by
	delete filterParams.order_by
	return [...key.slice(0, 4), filterParams, ...key.slice(5)]
}

const taskQuery = useTasks(
	() => taskScope.value ?? {},
	{
		enabled: () => authStore.authenticated && taskScope.value !== null,
		placeholderData: (previousData, previousQuery) => {
			if (!previousQuery || !taskScope.value) {
				return undefined
			}
			const sameFilters = hashKey(withoutSort(previousQuery.queryKey))
				=== hashKey(withoutSort(taskKeys.list(taskScope.value)))
			return sameFilters ? previousData : undefined
		},
	},
)
const tasks = taskQuery.tasks
const showNothingToDo = ref<boolean>(false)


setTimeout(() => showNothingToDo.value = true, 100)

const showAll = computed(() => typeof props.dateFrom === 'undefined' || typeof props.dateTo === 'undefined')

const filteredLabels = computed(() => {
	if (!props.labelIds || props.labelIds.length === 0) {
		return []
	}
	return props.labelIds
		.map(id => getLabelById(Number(id)))
		.filter(label => label !== null && label !== undefined)
})

const savedFilterIgnored = computed(() => {
	return filteredLabels.value.length > 0
		&& filterIdUsedOnOverview.value
		&& typeof projectList.projects[filterIdUsedOnOverview.value] !== 'undefined'
})

const pageTitle = computed(() => {
	// We need to define "key" because it is the first parameter in the array and we need the second
	const predefinedRange = Object.entries(DATE_RANGES)
		.find(([, value]) => props.dateFrom === value[0] && props.dateTo === value[1])
		?.[0]
	if (typeof predefinedRange !== 'undefined') {
		return t(`input.datepickerRange.ranges.${predefinedRange}`)
	}

	return showAll.value
		? t('task.show.titleCurrent')
		: t('task.show.fromuntil', {
			from: formatDate(props.dateFrom, 'LL'),
			until: formatDate(props.dateTo, 'LL'),
		})
})
const hasTasks = computed(() => tasks.value && tasks.value.length > 0)
const userAuthenticated = computed(() => authStore.authenticated)
const loading = taskQuery.isFetching
const showLoading = useDelayedLoading(loading)
const filterIdUsedOnOverview = computed(() => authStore.settings?.frontend_settings?.filter_id_used_on_overview)

interface dateStrings {
	dateFrom: Date | string | null,
	dateTo: Date | string | null,
}

function setDate(dates: dateStrings) {
	const from = dates.dateFrom ?? props.dateFrom
	const to = dates.dateTo ?? props.dateTo
	router.push({
		name: route.name as string,
		query: {
			from: from instanceof Date ? from.toISOString() : from,
			to: to instanceof Date ? to.toISOString() : to,
			showOverdue: props.showOverdue ? 'true' : 'false',
			showNulls: props.showNulls ? 'true' : 'false',
		},
	})
}

function setShowOverdue(show: boolean) {
	router.push({
		name: route.name as string,
		query: {
			...route.query,
			showOverdue: show ? 'true' : 'false',
		},
	})
}

function setShowNulls(show: boolean) {
	router.push({
		name: route.name as string,
		query: {
			...route.query,
			showNulls: show ? 'true' : 'false',
		},
	})
}

function clearLabelFilter() {
	emit('clearLabelFilter')
}

const sortParams = computed<Pick<TaskFilterParams, 'sort_by' | 'order_by'>>(() => {
	const sortBy = isTableView.value ? sortByActiveColumns(tableSortBy.value, tableColumns.value) : {}
	const fields = (Object.keys(sortBy) as (keyof SortBy)[]).filter(field => sortBy[field] !== 'none')
	if (fields.length === 0) {
		return {
			sort_by: ['due_date', 'id'],
			order_by: ['asc', 'desc'],
		}
	}

	return {
		sort_by: [...fields, 'id'],
		order_by: [...fields.map(field => sortBy[field] as string), 'desc'],
	}
})

async function loadPendingTasks(from: Date|string|undefined, to: Date|string|undefined, filterId: number | null | undefined) {
	// FIXME: HACK! This should never happen.
	// Since this route is authentication only, users would get an error message if they access the page unauthenticated.
	// Since this component is mounted as the home page before unauthenticated users get redirected
	// to the login page, they will almost always see the error message.
	if (!userAuthenticated.value) {
		return
	}

	const params: TaskFilterParams = {
		...sortParams.value,
		filter: 'done = false',
		filter_include_nulls: props.showNulls,
		q: '',
		expand: ['comment_count', 'is_unread', 'buckets'],
	}

	if (!showAll.value) {

		params.filter += ` && due_date < '${to instanceof Date ? to.toISOString() : to}'`

		// NOTE: Ideally we could also show tasks with a start or end date in the specified range, but the api
		//       is not capable (yet) of combining multiple filters with 'and' and 'or'.

		if (!props.showOverdue) {
			params.filter += ` && due_date > '${from instanceof Date ? from.toISOString() : from}'`
		}
	}

	// Add label filtering
	if (props.labelIds && props.labelIds.length > 0) {
		const labelFilter = `labels in ${props.labelIds.join(', ')}`
		params.filter += params.filter ? ` && ${labelFilter}` : labelFilter
	}

	let projectId = null
	if (showAll.value && filterId && typeof projectList.projects[filterId] !== 'undefined'
		&& (!props.labelIds || props.labelIds.length === 0)) {
		projectId = filterId
	}

	taskScope.value = {project: projectId, params: {...params, filter_timezone: authStore.settings.timezone}}
}

watch(taskQuery.data, data => { if (data) emit('tasksLoaded', true) })

function updateTasks() { return taskQuery.refetch() }

// Keep sidebar setting changes from reloading tasks.
watch(
	[
		() => props.dateFrom,
		() => props.dateTo,
		filterIdUsedOnOverview,
		() => props.showOverdue,
		() => props.showNulls,
		() => props.labelIds,
		sortParams,
	],
	([from, to, filterId]) => loadPendingTasks(from, to, filterId),
	{immediate: true},
)
watchEffect(() => setTitle(pageTitle.value))
</script>

<style lang="scss" scoped>
.tasks {
	list-style: none;
	margin: 0;
}

.show-tasks-options {
	display: flex;
	flex-direction: column;
}

.show-tasks-view-options {
	display: flex;
	align-items: center;
	gap: .5rem;
}

.view-mode-switch {
	display: inline-flex;
	gap: .5rem;
}

.llama-cool {
	margin: 3rem auto 0;
	display: block;
}

.label-filter-info {
	margin-block-end: 1rem;
	
	.clear-filter-button {
		margin-inline-start: auto;
		padding: 0.25rem 0.5rem;
		
		&:hover {
			color: var(--danger);
		}
	}

	:deep(.message.info) {
		inline-size: 100%;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
	}
}
</style>
