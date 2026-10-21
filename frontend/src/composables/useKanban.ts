import {computed, ref, toValue, watch, type MaybeRefOrGetter} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {klona} from 'klona/lite'
import {kanbanQuery, type BucketResponse} from '@/client/queries/kanban'
import type {TaskFilterParams} from '@/client/queries/tasks'
import {sortAggregateBuckets} from '@/helpers/kanbanSubprojects'

export function useKanban(
	project: MaybeRefOrGetter<number>,
	view: MaybeRefOrGetter<number>,
	params: MaybeRefOrGetter<TaskFilterParams>,
	enabled: MaybeRefOrGetter<boolean> = true,
) {
	const query = useQuery(computed(() =>
		kanbanQuery(toValue(project), toValue(view), toValue(params), toValue(enabled))))
	const shown = computed(() => {
		const data = query.data.value?.buckets ?? []
		return toValue(params).include_subprojects === true ? sortAggregateBuckets(data, toValue(project)) : data
	})
	const dragBuckets = ref<BucketResponse[] | null>(null)
	const buckets = computed(() => dragBuckets.value ?? shown.value)
	function startDrag() { dragBuckets.value = klona(shown.value) }
	function endDrag() { dragBuckets.value = null }
	watch([() => toValue(project), () => toValue(view), () => toValue(params)], endDrag)
	return {...query, buckets, dragBuckets, startDrag, endDrag}
}
