import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

const updateTask = vi.hoisted(() => ({mutateAsync: vi.fn()}))
vi.mock('@/client/queries/taskMutations', () => ({useUpdateTaskMutation: () => updateTask}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projects: {2: {title: 'Sidebar project'}}})}))
vi.mock('@/message', () => ({success: vi.fn(), error: vi.fn()}))
vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

import type {TaskResponse} from '@/client/queries/tasks'
import {useTaskDragState} from './useTaskDragState'
import {useTaskDragToProject} from './useTaskDragToProject'

const dragged = {id: 5, title: 'Dragged', project_id: 1} as TaskResponse
const dropEvent = {originalEvent: new MouseEvent('mouseup', {clientX: 10, clientY: 20})}

function element(tag: string, projectId: number, child?: HTMLElement) {
	const el = document.createElement(tag)
	el.dataset.projectId = String(projectId)
	if (child) el.append(child)
	return el
}

// jsdom has no layout, so the elements under the pointer are given.
function pointerOver(...elements: Element[]) {
	document.elementsFromPoint = vi.fn(() => elements)
}

describe('useTaskDragToProject', () => {
	beforeEach(() => {
		updateTask.mutateAsync.mockReset()
		updateTask.mutateAsync.mockResolvedValue({})
		useTaskDragState().setDraggedTask(dragged)
	})
	afterEach(() => useTaskDragState().setDraggedTask(null))

	it('moves the task to the sidebar project under the pointer', async () => {
		const label = document.createElement('span')
		pointerOver(label, element('li', 2, label))

		const result = await useTaskDragToProject().handleTaskDropToProject(dropEvent)

		expect(result).toEqual({moved: true, targetProjectId: 2})
		expect(updateTask.mutateAsync).toHaveBeenCalledWith(expect.objectContaining({id: 5, project_id: 2}))
	})

	it('ignores a card of another project under the pointer', async () => {
		const title = document.createElement('span')
		pointerOver(title, element('div', 2, title))

		const result = await useTaskDragToProject().handleTaskDropToProject(dropEvent)

		expect(result).toEqual({moved: false, targetProjectId: null})
		expect(updateTask.mutateAsync).not.toHaveBeenCalled()
	})

	it('finds the sidebar project below a card that is dragged over it', async () => {
		pointerOver(element('div', 3), element('li', 2))

		const result = await useTaskDragToProject().handleTaskDropToProject(dropEvent)

		expect(result).toEqual({moved: true, targetProjectId: 2})
	})
})
