import {shallowMount, flushPromises} from '@vue/test-utils'
import {afterEach, describe, expect, it, vi, beforeEach} from 'vitest'
import {createPinia} from 'pinia'
import {nextTick} from 'vue'
import draggable from 'zhyswan-vuedraggable'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	projectViewBucketsTasksList: vi.fn(),
	projectViewTasksList: vi.fn(),
	bucketsUpdate: vi.fn(),
	tasksCreate: vi.fn(),
	taskBucketUpdate: vi.fn(),
	tasksPositionUpdate: vi.fn(),
}))
const {projectViewBucketsTasksList: loadBoard, projectViewTasksList: loadBucketPage, bucketsUpdate: updateBucket} = sdk

// Asymmetric positions: the midpoint between the neighbours must differ from the dragged bucket's own position.
const buckets = [
	{id: 1, title: 'First', position: 100, tasks: [{id: 11, title: 'Task 11'}], count: 1, limit: 0},
	{id: 2, title: 'Second', position: 250, tasks: [], count: 0, limit: 0},
	{id: 3, title: 'Third', position: 300, tasks: [], count: 0, limit: 0},
]

vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))

const aggregate = vi.hoisted(() => ({state: {value: false as boolean | undefined}}))
vi.mock('@/composables/useIncludeSubprojects', async () => {
	const {computed, reactive} = await import('vue')
	aggregate.state = reactive(aggregate.state)
	return {useIncludeSubprojects: () => computed(() => aggregate.state.value)}
})
vi.mock('@/composables/useCurrentProject', () => ({
	useCurrentProject: () => ({
		currentProject: {
			value: {
				id: 1,
				title: 'Test',
				max_permission: 2,
				views: [{id: 10, view_kind: 'kanban', bucket_configuration_mode: 'manual'}],
			},
		},
		isPending: {value: false},
	}),
}))

const drop = vi.hoisted(() => ({
	result: {moved: false, targetProjectId: null as number | null},
	task: {id: 11, title: 'Task 11'},
}))
vi.mock('@/composables/useTaskDragToProject', () => ({
	useTaskDragToProject: () => ({
		handleTaskDropToProject: async (
			_e: unknown,
			onSuccess?: (task: unknown, targetProjectId: number) => void,
		) => {
			if (drop.result.moved) onSuccess?.(drop.task, drop.result.targetProjectId!)
			return drop.result
		},
	}),
}))

vi.mock('@/stores/auth', () => ({
	useAuthStore: () => ({
		settings: {
			frontend_settings: {
				always_show_bucket_task_count: false,
				quick_add_magic_mode: 'vikunja',
				quick_add_default_reminders: [],
			},
		},
	}),
}))

vi.mock('@/client/queries/savedFilters', () => ({
	savedFilterQuery: (id: number) => ({
		queryKey: ['saved-filters', 'detail', id],
		queryFn: async () => null,
		enabled: false,
	}),
}))

vi.mock('@vueuse/router', () => ({
	useRouteQuery: () => ({value: undefined}),
}))

vi.mock('vue-router', () => ({
	useRouter: () => ({push: vi.fn(), currentRoute: {value: {fullPath: '/', params: {}}}}),
}))

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

import ProjectKanban from './ProjectKanban.vue'

let queryClient: QueryClient
const mounted: { unmount: () => void }[] = []
afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()))

async function mountKanban(stubs: Record<string, unknown> = {}, projectId = 1) {
	queryClient = new QueryClient()
	const wrapper = shallowMount(ProjectKanban, {
		props: {
			isLoadingProject: false,
			projectId,
			viewId: 10,
		},
		global: {
			plugins: [createPinia(), [VueQueryPlugin, {queryClient}]],
			mocks: {$t: (key: string) => key},
			directives: {focus: () => {}, tooltip: () => {}, cy: () => {}},
			stubs: {
				ProjectWrapper: {template: '<div><slot name="default"/></div>'},
				...stubs,
			},
		},
	})

	mounted.push(wrapper)
	await flushPromises()
	return wrapper
}

// vuedraggable is stubbed away by shallowMount, but the add task control lives in its footer slot.
const DRAGGABLE_STUB = {
	props: ['modelValue'],
	template: '<ul><template v-for="(element, index) in modelValue"><slot name="item" :element="element" :index="index" /></template><slot name="footer" /></ul>',
}

function dragEndEvent(bucketId: string) {
	const item = document.createElement('li')
	item.dataset.bucketId = bucketId
	// The DOM index sortable reports does not have to be a valid bucket index
	return {item, newIndex: 42}
}

function taskDragEndEvent(taskId: number, bucketIndex = 0) {
	const item = document.createElement('li')
	item.dataset.taskId = String(taskId)
	const list = document.createElement('ul')
	list.dataset.bucketIndex = String(bucketIndex)
	return {item, to: list, from: list, originalEvent: new MouseEvent('mouseup'), newIndex: 0}
}

// The bucket draggable comes first, each bucket renders its own task draggable after it.
function taskDraggable(wrapper: Awaited<ReturnType<typeof mountKanban>>) {
	return wrapper.findAllComponents(draggable)[1]
}

function cachedBoard() {
	const entries = queryClient.getQueriesData<{
		buckets: {id: number, tasks: {id: number}[], count: number}[],
	}>({queryKey: ['kanban']})
	return entries[0][1]
}

describe('ProjectKanban', () => {
	beforeEach(() => {
		updateBucket.mockClear()
		updateBucket.mockResolvedValue({data: {id: 2, position: 200}})
		loadBoard.mockClear()
		loadBoard.mockResolvedValue({data: {items: buckets}})
		loadBucketPage.mockClear()
		loadBucketPage.mockResolvedValue({data: {items: []}})
		sdk.tasksCreate.mockReset()
		sdk.taskBucketUpdate.mockReset()
		sdk.tasksPositionUpdate.mockReset()
		aggregate.state.value = false
		drop.result = {moved: false, targetProjectId: null}
	})

	it('saves the position of the dropped bucket', async () => {
		const wrapper = await mountKanban()

		wrapper.findComponent(draggable).vm.$emit('end', dragEndEvent('2'))
		await flushPromises()

		expect(updateBucket).toHaveBeenCalledWith(expect.objectContaining({
			path: {project: 1, view: 10, bucket: 2},
			body: expect.objectContaining({position: 200}),
		}))
	})

	it('keeps the add bucket column while the board refetches in the background', async () => {
		const wrapper = await mountKanban()
		expect(wrapper.find('.new-bucket').exists()).toBe(true)

		loadBoard.mockReturnValue(new Promise(() => {}))
		queryClient.refetchQueries()
		await nextTick()

		expect(wrapper.find('.new-bucket').exists()).toBe(true)
	})

	it('does not load another bucket page while a task is dragged out of the bucket', async () => {
		const wrapper = await mountKanban()
		// The scroll handler is wired through vuedraggable's component-data, which the stub does not render.
		const vm = wrapper.vm as unknown as {
			updateTasks: (bucketId: number, tasks: unknown[]) => void,
			handleTaskContainerScroll: (id: number, el: HTMLElement) => void,
		}

		vm.updateTasks(1, [])
		vm.handleTaskContainerScroll(1, document.createElement('div'))
		await flushPromises()

		expect(loadBucketPage).not.toHaveBeenCalled()
	})

	it('marks the board as dragging while a task is dragged', async () => {
		const wrapper = await mountKanban({BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB})
		const event = taskDragEndEvent(11)

		taskDraggable(wrapper).vm.$emit('start', event)
		await nextTick()
		expect(wrapper.find('.kanban').classes()).toContain('is-dragging')

		taskDraggable(wrapper).vm.$emit('end', event)
		await flushPromises()
		expect(wrapper.find('.kanban').classes()).not.toContain('is-dragging')
	})

	it('marks the board as dragging while a bucket is dragged', async () => {
		const wrapper = await mountKanban({BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB})

		wrapper.findAllComponents(draggable)[0].vm.$emit('start')
		await nextTick()
		expect(wrapper.find('.kanban').classes()).toContain('is-dragging')

		wrapper.findAllComponents(draggable)[0].vm.$emit('end', dragEndEvent('2'))
		await flushPromises()
		expect(wrapper.find('.kanban').classes()).not.toContain('is-dragging')
	})

	it('does not refetch the board after a task was dropped on another project', async () => {
		const wrapper = await mountKanban({BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB})
		drop.result = {moved: true, targetProjectId: 2}
		loadBoard.mockClear()

		taskDraggable(wrapper).vm.$emit('end', taskDragEndEvent(11))
		await flushPromises()

		expect(loadBoard).not.toHaveBeenCalled()
	})

	it('drops the card from a saved filter board when the task moved to another project', async () => {
		const wrapper = await mountKanban({BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB}, -2)
		drop.result = {moved: true, targetProjectId: 2}

		taskDraggable(wrapper).vm.$emit('end', taskDragEndEvent(11))
		await flushPromises()

		expect(cachedBoard()?.buckets[0].tasks).toEqual([])
		expect(cachedBoard()?.buckets[0].count).toBe(0)
	})

	it('leaves a real project board to the mutation when the task moved to another project', async () => {
		const wrapper = await mountKanban({BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB})
		drop.result = {moved: true, targetProjectId: 2}

		taskDraggable(wrapper).vm.$emit('end', taskDragEndEvent(11))
		await flushPromises()

		expect(cachedBoard()?.buckets[0].tasks).toHaveLength(1)
		expect(cachedBoard()?.buckets[0].count).toBe(1)
	})

	it('re-enables the add task input once the create settles, without a board request', async () => {
		const wrapper = await mountKanban({BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB})
		let resolveCreate!: (value: unknown) => void
		sdk.tasksCreate.mockImplementation(() => new Promise(resolve => { resolveCreate = resolve }))

		;(wrapper.vm as unknown as {toggleShowNewTaskInput: (id: number) => void}).toggleShowNewTaskInput(1)
		await nextTick()
		await wrapper.find('.bucket-footer input').setValue('New task')
		await wrapper.find('.bucket-footer input').trigger('keyup.enter')
		await flushPromises()
		expect(wrapper.find('.bucket-footer input').attributes('disabled')).toBeDefined()

		loadBoard.mockClear()
		resolveCreate({data: {id: 12, title: 'New task', project_id: 1, bucket_id: 1}})
		await flushPromises()

		expect(wrapper.find('.bucket-footer input').attributes('disabled')).toBeUndefined()
		expect(loadBoard).not.toHaveBeenCalled()
	})

	it('closes the add task input when the created task fills the bucket limit', async () => {
		loadBoard.mockResolvedValue({data: {items: [{...buckets[0], count: 1, limit: 2}, buckets[1], buckets[2]]}})
		sdk.tasksCreate.mockResolvedValue({data: {id: 12, title: 'New task', project_id: 1, bucket_id: 1}})
		const wrapper = await mountKanban({BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB})

		;(wrapper.vm as unknown as {toggleShowNewTaskInput: (id: number) => void}).toggleShowNewTaskInput(1)
		await nextTick()
		await wrapper.find('.bucket-footer input').setValue('New task')
		await wrapper.find('.bucket-footer input').trigger('keyup.enter')
		await flushPromises()

		expect(wrapper.find('.bucket-footer input').exists()).toBe(false)
	})

	it('does nothing when the dropped bucket is gone', async () => {
		const wrapper = await mountKanban()

		wrapper.findComponent(draggable).vm.$emit('end', dragEndEvent('42'))

		expect(updateBucket).not.toHaveBeenCalled()
	})

	describe('on a parent project that includes its sub-projects', () => {
		const STUBS = {BucketDraggable: DRAGGABLE_STUB, TaskDraggable: DRAGGABLE_STUB}
		const own = {id: 11, title: 'Own', project_id: 1, position: 100}
		const child = {id: 21, title: 'Child', project_id: 2, position: 0}
		const board = (first: unknown[] = [own, child]) => ({
			data: {items: [{...buckets[0], tasks: first, count: first.length}, buckets[1], buckets[2]]},
		})

		const fullFirstBucket = () => ({
			data: {items: [{...buckets[0], tasks: [own], count: 2, limit: 2}, buckets[1], buckets[2]]},
		})

		type Vm = {updateTasks: (bucketId: number, tasks: unknown[]) => void}

		// Mimics sortable: the card leaves the first column and shows up in the target column.
		async function dragTask(
			wrapper: Awaited<ReturnType<typeof mountKanban>>,
			taskId: number,
			targetIndex: number,
		) {
			const vm = wrapper.vm as unknown as Vm
			taskDraggable(wrapper).vm.$emit('start', taskDragEndEvent(taskId, 0))
			await nextTick()
			const card = [own, child].find(item => item.id === taskId)
			if (targetIndex !== 0) {
				vm.updateTasks(1, [own, child].filter(item => item.id !== taskId))
				vm.updateTasks(buckets[targetIndex].id, [card])
			}
			taskDraggable(wrapper).vm.$emit('end', taskDragEndEvent(taskId, targetIndex))
			await flushPromises()
		}

		beforeEach(() => {
			aggregate.state.value = true
			loadBoard.mockResolvedValue(board())
			sdk.taskBucketUpdate.mockImplementation(async ({path}: {path: {bucket: number}}) => ({
				data: {bucket_id: path.bucket, task: child},
			}))
			sdk.tasksPositionUpdate.mockResolvedValue({data: {position: 10}})
		})

		it('asks for the sub-project tasks when the project has live sub-projects', async () => {
			await mountKanban(STUBS)
			expect(loadBoard).toHaveBeenCalledOnce()
			expect(loadBoard).toHaveBeenCalledWith(expect.objectContaining({
				query: expect.objectContaining({include_subprojects: true}),
			}))
		})

		it('does not send the flag for a project without live sub-projects', async () => {
			aggregate.state.value = false
			await mountKanban(STUBS)
			expect(loadBoard).toHaveBeenCalledOnce()
			expect(loadBoard.mock.calls[0][0].query).not.toHaveProperty('include_subprojects')
		})

		it('loads no board until it is known whether the project has sub-projects', async () => {
			aggregate.state.value = undefined
			await mountKanban(STUBS)
			expect(loadBoard).not.toHaveBeenCalled()

			aggregate.state.value = true
			await flushPromises()
			expect(loadBoard).toHaveBeenCalledOnce()
			expect(loadBoard).toHaveBeenCalledWith(expect.objectContaining({
				query: expect.objectContaining({include_subprojects: true}),
			}))
		})

		it('asks for more cards of a column with the flag as well', async () => {
			loadBoard.mockResolvedValue({data: {items: [{...buckets[0], tasks: [own], count: 30}, buckets[1], buckets[2]]}})
			const wrapper = await mountKanban(STUBS)
			const el = document.createElement('div')

			;(wrapper.vm as unknown as {handleTaskContainerScroll: (id: number, el: HTMLElement) => void})
				.handleTaskContainerScroll(1, el)
			await flushPromises()

			expect(loadBucketPage).toHaveBeenCalledWith(expect.objectContaining({
				query: expect.objectContaining({include_subprojects: true, page: 2}),
			}))
		})

		it('shows the own cards by position before the sub-project cards', async () => {
			const late = {id: 12, title: 'Late', project_id: 1, position: 300}
			loadBoard.mockResolvedValue(board([child, late, own]))
			const wrapper = await mountKanban(STUBS)

			expect(wrapper.findAll('.bucket')[0].findAll('.task-item').map(item => Number(item.attributes('data-task-id'))))
				.toEqual([11, 12, 21])
		})

		it('moves a dropped sub-project card to the other column without sending a position', async () => {
			const wrapper = await mountKanban(STUBS)

			await dragTask(wrapper, 21, 1)

			expect(sdk.taskBucketUpdate).toHaveBeenCalledWith(expect.objectContaining({
				path: {project: 1, view: 10, bucket: 2},
				body: {task_id: 21},
			}))
			expect(sdk.tasksPositionUpdate).not.toHaveBeenCalled()
		})

		it('does not reorder an own card either when it changes the column', async () => {
			sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: 2, task: own}})
			const wrapper = await mountKanban(STUBS)

			await dragTask(wrapper, 11, 1)

			expect(sdk.taskBucketUpdate).toHaveBeenCalledOnce()
			expect(sdk.tasksPositionUpdate).not.toHaveBeenCalled()
		})

		it('sends nothing for a card dropped in its own column and puts it back', async () => {
			const wrapper = await mountKanban(STUBS)

			await dragTask(wrapper, 21, 0)

			expect(sdk.taskBucketUpdate).not.toHaveBeenCalled()
			expect(sdk.tasksPositionUpdate).not.toHaveBeenCalled()
			expect(wrapper.find('.kanban').classes()).not.toContain('is-dragging')
			expect(cachedBoard()?.buckets[0].tasks.map(item => item.id)).toEqual([11, 21])
		})

		it('puts a card back when the server refuses the move', async () => {
			sdk.taskBucketUpdate.mockRejectedValue({code: 10900})
			const wrapper = await mountKanban(STUBS)

			await dragTask(wrapper, 21, 1)

			expect(sdk.taskBucketUpdate).toHaveBeenCalledOnce()
			expect(cachedBoard()?.buckets[0].tasks.map(item => item.id)).toEqual([11, 21])
			expect(cachedBoard()?.buckets[1].tasks).toEqual([])
		})

		it('leaves the limit of a column to the server', async () => {
			loadBoard.mockResolvedValue(fullFirstBucket())
			const wrapper = await mountKanban(STUBS)

			const header = wrapper.find('.bucket-header .limit')
			expect(header.text()).toBe('2')
			expect(header.classes()).not.toContain('is-max')
			expect(wrapper.find('.bucket-footer xbutton').attributes('disabled')).toBe('false')
			const vm = wrapper.vm as unknown as {shouldAcceptDrop: (bucket: unknown) => boolean}
			expect(vm.shouldAcceptDrop({...buckets[1], limit: 2, count: 2})).toBe(true)
		})

		it('still enforces the limit of a column on a project without sub-projects', async () => {
			aggregate.state.value = false
			loadBoard.mockResolvedValue(fullFirstBucket())
			const wrapper = await mountKanban(STUBS)

			const header = wrapper.find('.bucket-header .limit')
			expect(header.text()).toBe('2/2')
			expect(header.classes()).toContain('is-max')
			expect(wrapper.find('.bucket-footer xbutton').attributes('disabled')).toBe('true')
			const vm = wrapper.vm as unknown as {shouldAcceptDrop: (bucket: unknown) => boolean}
			expect(vm.shouldAcceptDrop({...buckets[1], limit: 2, count: 2})).toBe(false)
		})

		it('reads the board again after a bucket was renamed', async () => {
			updateBucket.mockResolvedValue({data: {id: 1, title: 'Renamed', position: 100}})
			const wrapper = await mountKanban(STUBS)
			loadBoard.mockClear()

			await (wrapper.vm as unknown as {saveBucketTitle: (id: number, title: string) => Promise<void>})
				.saveBucketTitle(1, 'Renamed')
			await flushPromises()

			expect(loadBoard).toHaveBeenCalledOnce()
		})

		it('reads the board again after a card was dropped on a sidebar project', async () => {
			const wrapper = await mountKanban(STUBS)
			drop.result = {moved: true, targetProjectId: 2}
			loadBoard.mockClear()

			taskDraggable(wrapper).vm.$emit('end', taskDragEndEvent(11))
			await flushPromises()

			expect(loadBoard).toHaveBeenCalledOnce()
		})
	})
})
