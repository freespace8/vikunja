<template>
	<Popup
		placement="bottom-start"
		:anchor="triggerEl"
	>
		<template #trigger="{toggle}">
			<XButton
				ref="trigger"
				icon="th"
				variant="secondary"
				class="mie-2"
				@click.prevent.stop="toggle()"
			>
				{{ $t('project.table.columns') }}
			</XButton>
		</template>
		<template #content="{isOpen}">
			<Card
				class="columns-filter"
				:class="{'is-open': isOpen}"
			>
				<FancyCheckbox
					v-for="column in PICKER_COLUMNS"
					:key="column.key"
					:model-value="modelValue[column.key]"
					@update:modelValue="value => emit('update:modelValue', {...modelValue, [column.key]: value})"
				>
					{{ column.label ?? $t(`task.attributes.${column.key}`) }}
				</FancyCheckbox>
			</Card>
		</template>
	</Popup>
</template>

<script setup lang="ts">
import {computed, ref, type ComponentPublicInstance} from 'vue'

import FancyCheckbox from '@/components/input/FancyCheckbox.vue'
import Popup from '@/components/misc/Popup.vue'
import type {TaskTableColumns} from '@/components/tasks/taskTableColumns'

defineProps<{
	modelValue: TaskTableColumns,
}>()

const emit = defineEmits<{
	'update:modelValue': [value: TaskTableColumns],
}>()

const PICKER_COLUMNS: {key: keyof TaskTableColumns, label?: string}[] = [
	{key: 'index', label: '#'},
	{key: 'done'},
	{key: 'project'},
	{key: 'title'},
	{key: 'priority'},
	{key: 'labels'},
	{key: 'assignees'},
	{key: 'commentCount'},
	{key: 'dueDate'},
	{key: 'startDate'},
	{key: 'endDate'},
	{key: 'percentDone'},
	{key: 'doneAt'},
	{key: 'created'},
	{key: 'updated'},
	{key: 'createdBy'},
]

const trigger = ref<ComponentPublicInstance | null>(null)
const triggerEl = computed<HTMLElement | null>(() => (trigger.value?.$el as HTMLElement) ?? null)
</script>

<style lang="scss" scoped>
.columns-filter {
	margin: 0;

	:deep(.card-content .content) {
		display: flex;
		flex-direction: column;
	}
}
</style>
