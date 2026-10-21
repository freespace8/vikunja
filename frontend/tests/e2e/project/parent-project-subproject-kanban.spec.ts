import {test, expect} from '../../support/fixtures'
import {BucketFactory} from '../../factories/bucket'
import {ProjectFactory} from '../../factories/project'
import {ProjectViewFactory} from '../../factories/project_view'
import {TaskBucketFactory} from '../../factories/task_buckets'
import {TaskFactory} from '../../factories/task'

const PARENT = {project: 100, view: 104, buckets: [1001, 1002, 1003]}
const CHILD = {project: 101, view: 108, buckets: [2001, 2002, 2003]}

async function createBoardTree() {
	await ProjectFactory.create(1, {id: PARENT.project, title: 'Parent Project'})
	await ProjectFactory.create(1, {
		id: CHILD.project,
		title: 'Child Project',
		parent_project_id: PARENT.project,
	}, false)

	// Each project has To-Do, Doing and Done with the done role on the last one. The titles match, so the
	// parent's columns hold the cards of the child's columns of the same name.
	for (const [index, board] of [PARENT, CHILD].entries()) {
		await ProjectViewFactory.create(1, {
			id: board.view,
			project_id: board.project,
			title: 'Kanban',
			view_kind: 3,
			bucket_configuration_mode: 1,
			done_bucket_id: board.buckets[2],
		}, index === 0)
		for (const [position, title] of ['To-Do', 'Doing', 'Done'].entries()) {
			await BucketFactory.create(1, {
				id: board.buckets[position],
				title,
				project_view_id: board.view,
				// Asymmetric, so a position that is computed from the neighbours cannot match by accident.
				position: [100, 250, 300][position],
			}, index === 0 && position === 0)
		}
	}

	const [parentTask] = await TaskFactory.create(1, {id: 1, project_id: PARENT.project, title: 'Parent task'})
	const [childTask] = await TaskFactory.create(1, {id: 2, project_id: CHILD.project, title: 'Child task'}, false)
	await TaskBucketFactory.create(1, {
		task_id: parentTask.id,
		bucket_id: PARENT.buckets[1],
		project_view_id: PARENT.view,
	})
	await TaskBucketFactory.create(1, {
		task_id: childTask.id,
		bucket_id: CHILD.buckets[1],
		project_view_id: CHILD.view,
	}, false)

	const [todoChildTask] = await TaskFactory.create(1, {id: 3, project_id: CHILD.project, title: 'Child to-do'}, false)
	await TaskBucketFactory.create(1, {
		task_id: todoChildTask.id,
		bucket_id: CHILD.buckets[0],
		project_view_id: CHILD.view,
	}, false)

	return {parentTask, childTask}
}

const column = (page, position: number) => page.locator(`.kanban .bucket:nth-child(${position}) .tasks`)
const card = (page, title: string) => page.locator('.kanban .bucket .tasks .task').filter({hasText: title}).first()

test.describe('Kanban board of a parent project with sub-projects', () => {
	test('Shows a sub-project task in the column with the same name and moves it on its own board', async ({
		authenticatedPage: page,
		apiContext,
		userToken,
	}) => {
		const {childTask} = await createBoardTree()
		const headers = {Authorization: `Bearer ${userToken}`}

		await page.goto(`/projects/${PARENT.project}/${PARENT.view}`)

		await expect(column(page, 2)).toContainText('Parent task')
		await expect(column(page, 2)).toContainText('Child task')
		await expect(card(page, 'Child task').locator('.kanban-card__project')).toHaveText('Child Project')
		await expect(card(page, 'Parent task').locator('.kanban-card__project')).toHaveText('Parent Project')

		const move = page.waitForResponse(response =>
			new URL(response.url()).pathname === `/api/v2/projects/${PARENT.project}/views/${PARENT.view}/buckets/${PARENT.buckets[2]}/tasks`
			&& response.request().method() === 'PUT',
		)
		await card(page, 'Child task').dragTo(column(page, 3))
		expect((await move).ok()).toBeTruthy()

		await expect(column(page, 3)).toContainText('Child task')
		await expect(column(page, 2)).not.toContainText('Child task')
		await expect(column(page, 2)).toContainText('Parent task')

		await page.reload()
		await expect(column(page, 3)).toContainText('Child task')
		await expect(column(page, 2)).not.toContainText('Child task')

		const stored = await apiContext.get(`tasks/${childTask.id}`, {headers})
		expect(stored.ok()).toBe(true)
		const task = await stored.json()
		expect(task.done).toBe(true)
		expect(task.project_id).toBe(CHILD.project)

		await page.goto(`/projects/${CHILD.project}/${CHILD.view}`)
		await expect(column(page, 3)).toContainText('Child task')
		await expect(column(page, 2)).not.toContainText('Child task')
	})

	test('Keeps the project of a task that is dropped over a card of another project', async ({
		authenticatedPage: page,
		apiContext,
		userToken,
	}) => {
		const {parentTask} = await createBoardTree()
		const headers = {Authorization: `Bearer ${userToken}`}
		await page.goto(`/projects/${PARENT.project}/${PARENT.view}`)
		await expect(column(page, 1)).toContainText('Child to-do')

		const move = page.waitForResponse(response =>
			new URL(response.url()).pathname === `/api/v2/projects/${PARENT.project}/views/${PARENT.view}/buckets/${PARENT.buckets[0]}/tasks`
			&& response.request().method() === 'PUT',
		)
		const patches: string[] = []
		page.on('request', request => {
			if (request.method() === 'PATCH') patches.push(new URL(request.url()).pathname)
		})
		// The pointer is over the card of the sub-project when the card is released.
		await card(page, 'Parent task').dragTo(card(page, 'Child to-do'))
		expect((await move).ok()).toBeTruthy()
		await page.waitForLoadState('networkidle')

		await expect(column(page, 1)).toContainText('Parent task')
		expect(patches).toEqual([])

		const stored = await apiContext.get(`tasks/${parentTask.id}`, {headers})
		expect(stored.ok()).toBe(true)
		expect((await stored.json()).project_id).toBe(PARENT.project)
	})
})
