import {describe, it, expect, vi, beforeEach} from 'vitest'
import {shallowMount, flushPromises} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {createRouter, createMemoryHistory} from 'vue-router'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

import type {Task as ITask} from '@/client/generated'

const sdk = vi.hoisted(() => ({
	patchTasksRead: vi.fn(),
}))

vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/helpers/playPop', () => ({playPopSound: vi.fn()}))
const projects = vi.hoisted(() => ({current: {} as Record<number, unknown>}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projects: projects.current})}))
vi.mock('@/composables/useCurrentProject', () => ({useCurrentProject: () => ({currentProject: {value: null}})}))
vi.mock('vue-i18n', async importOriginal => ({
	...(await importOriginal<typeof import('vue-i18n')>()),
	useI18n: () => ({t: (key: string) => key}),
}))

import SingleTaskInProject from './SingleTaskInProject.vue'

const theTask = {id: 7, project_id: 1, title: 'Water the plants', done: true, is_favorite: false} as ITask

function deferred() {
	let resolve!: (value: unknown) => void
	const promise = new Promise(done => resolve = done)
	return {promise, resolve}
}

function mountRow(task: ITask = theTask) {
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [
			{path: '/tasks/:id', name: 'task.detail', component: {render: () => null}},
			{path: '/projects/:projectId', name: 'project.index', component: {render: () => null}},
		],
	})
	return shallowMount(SingleTaskInProject, {
		props: {theTask: task},
		global: {
			plugins: [createPinia(), router, [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			directives: {tooltip: {}},
			mocks: {$t: (key: string) => key},
		},
	})
}

describe('SingleTaskInProject', () => {
	beforeEach(() => {
		setActivePinia(createPinia())
		sdk.patchTasksRead.mockReset()
		projects.current = {}
	})

	describe('bucket badge', () => {
		const kanbanProject = {
			id: 1,
			title: 'Project',
			views: [
				{id: 10, position: 1, view_kind: 'list'},
				{id: 20, position: 2, view_kind: 'kanban'},
			],
		}
		const doing = {id: 3, project_view_id: 20, title: 'Doing'}

		it('shows the bucket of the kanban view of the task project', () => {
			projects.current = {1: kanbanProject}

			const wrapper = mountRow({...theTask, buckets: [doing]})

			expect(wrapper.get('.task-bucket').text()).toBe('Doing')
		})

		it('shows the bucket of a done task as well', () => {
			projects.current = {1: kanbanProject}

			const wrapper = mountRow({...theTask, done: true, buckets: [{...doing, title: 'Done'}]})

			expect(wrapper.get('.task-bucket').text()).toBe('Done')
		})

		it('shows nothing when the project has no kanban view', () => {
			projects.current = {1: {...kanbanProject, views: [kanbanProject.views[0]]}}

			const wrapper = mountRow({...theTask, buckets: [doing]})

			expect(wrapper.find('.task-bucket').exists()).toBe(false)
		})

		it('shows nothing when the task has no bucket or the buckets were not requested', () => {
			projects.current = {1: kanbanProject}

			expect(mountRow({...theTask, buckets: []}).find('.task-bucket').exists()).toBe(false)
			expect(mountRow().find('.task-bucket').exists()).toBe(false)
		})
	})

	it('shows the row as loading while marking it undone is in flight', async () => {
		const update = deferred()
		sdk.patchTasksRead.mockReturnValue(update.promise)
		const wrapper = mountRow()

		wrapper.getComponent({name: 'FancyCheckbox'}).vm.$emit('update:modelValue', false)
		await flushPromises()

		await vi.waitFor(() => expect(wrapper.get('.task').classes()).toContain('is-loading'))

		update.resolve({data: {...theTask, done: false}})
		await flushPromises()

		expect(wrapper.get('.task').classes()).not.toContain('is-loading')
	})

	it('shows the row as loading while favouring it is in flight', async () => {
		const favorite = deferred()
		sdk.patchTasksRead.mockReturnValue(favorite.promise)
		const wrapper = mountRow()

		await wrapper.get('.favorite').trigger('click')
		await flushPromises()

		expect(sdk.patchTasksRead).toHaveBeenCalledWith(expect.objectContaining({
			path: {task: 7},
			body: [expect.objectContaining({path: '/is_favorite', value: true})],
		}))
		await vi.waitFor(() => expect(wrapper.get('.task').classes()).toContain('is-loading'))

		favorite.resolve({data: {...theTask, is_favorite: true}})
		await flushPromises()

		expect(wrapper.get('.task').classes()).not.toContain('is-loading')
	})
})
