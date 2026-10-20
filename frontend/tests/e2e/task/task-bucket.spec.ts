import {test, expect} from '../../support/fixtures'
import {BucketFactory} from '../../factories/bucket'
import {ProjectFactory} from '../../factories/project'
import {ProjectViewFactory} from '../../factories/project_view'
import {TaskBucketFactory} from '../../factories/task_buckets'
import {TaskFactory} from '../../factories/task'

const BUCKET_TITLES = ['To-Do', 'Doing', 'Done']

async function seedBuckets({doneBucket = false} = {}) {
	const dueSoon = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString()
	const project = (await ProjectFactory.create(1, {
		id: 1,
		title: 'Bucket Project',
	}))[0]
	await ProjectViewFactory.create(1, {
		id: 1,
		project_id: project.id,
		title: 'List',
		view_kind: 0,
	})
	await ProjectViewFactory.create(1, {
		id: 2,
		project_id: project.id,
		title: 'Table',
		view_kind: 2,
	}, false)
	await ProjectViewFactory.create(1, {
		id: 3,
		project_id: project.id,
		title: 'Kanban',
		view_kind: 3,
		bucket_configuration_mode: 1,
		...(doneBucket ? {done_bucket_id: 3} : {}),
	}, false)
	const buckets = await BucketFactory.create(BUCKET_TITLES.length, {
		id: (i: number) => i,
		title: (i: number) => BUCKET_TITLES[i - 1],
		project_view_id: 3,
	})
	await TaskFactory.create(1, {
		id: 1,
		project_id: project.id,
		title: 'Doing task',
		due_date: dueSoon,
	})
	await TaskFactory.create(1, {
		id: 2,
		project_id: project.id,
		title: 'Backlog task',
		due_date: dueSoon,
	}, false)
	await TaskBucketFactory.create(1, {
		task_id: 1,
		bucket_id: buckets[1].id,
		project_view_id: 3,
	})
	await TaskBucketFactory.create(1, {
		task_id: 2,
		bucket_id: buckets[0].id,
		project_view_id: 3,
	}, false)

	const plain = (await ProjectFactory.create(1, {
		id: 2,
		title: 'Plain Project',
	}, false))[0]
	await ProjectViewFactory.create(1, {
		id: 4,
		project_id: plain.id,
		title: 'List',
		view_kind: 0,
	}, false)
	await ProjectViewFactory.create(1, {
		id: 5,
		project_id: plain.id,
		title: 'Table',
		view_kind: 2,
	}, false)
	await TaskFactory.create(1, {
		id: 3,
		project_id: plain.id,
		title: 'Plain task',
		due_date: dueSoon,
	}, false)
}

test.describe('Task bucket', () => {
	test('Overview list shows the bucket of each task and nothing without a kanban view', async ({
		authenticatedPage: page,
	}) => {
		await seedBuckets()

		await page.goto('/')

		const tasks = page.locator('[data-cy="showTasks"] .card .task')
		await expect(tasks.filter({hasText: 'Doing task'}).locator('.task-bucket')).toHaveText('Doing')
		await expect(tasks.filter({hasText: 'Backlog task'}).locator('.task-bucket')).toHaveText('To-Do')
		await expect(tasks.filter({hasText: 'Plain task'})).toBeVisible()
		await expect(tasks.filter({hasText: 'Plain task'}).locator('.task-bucket')).toHaveCount(0)
	})

	test('Overview table has a bucket column', async ({authenticatedPage: page}) => {
		await seedBuckets()
		await page.goto('/')
		const overview = page.locator('[data-cy="showTasks"]')

		await overview.getByRole('button', {name: 'Table', exact: true}).click()

		const table = overview.locator('table.table')
		await expect(table.locator('th').filter({hasText: 'Bucket'})).toBeVisible()
		const cells = (title: string) => table.locator('tbody tr').filter({hasText: title}).locator('td')
		await expect(cells('Doing task').filter({hasText: /^Doing$/})).toHaveCount(1)
		await expect(cells('Backlog task').filter({hasText: /^To-Do$/})).toHaveCount(1)
		await expect(cells('Plain task').filter({hasText: /^(Doing|To-Do|Done)$/})).toHaveCount(0)
	})

	test('Project list shows the bucket of each task', async ({authenticatedPage: page}) => {
		await seedBuckets()

		await page.goto('/projects/1/1')

		const tasks = page.locator('.list-view .single-task')
		await expect(tasks.filter({hasText: 'Doing task'}).locator('.task-bucket')).toHaveText('Doing')
		await expect(tasks.filter({hasText: 'Backlog task'}).locator('.task-bucket')).toHaveText('To-Do')
	})

	test('Project list of a project without a kanban view shows no bucket', async ({authenticatedPage: page}) => {
		await seedBuckets()

		await page.goto('/projects/2/4')

		const tasks = page.locator('.list-view .single-task')
		await expect(tasks.filter({hasText: 'Plain task'})).toBeVisible()
		await expect(page.locator('.task-bucket')).toHaveCount(0)
	})

	test('Project table has a bucket column that can be switched off', async ({authenticatedPage: page}) => {
		await seedBuckets()

		await page.goto('/projects/1/2')

		const table = page.locator('.project-table table.table')
		await expect(table.locator('th').filter({hasText: 'Bucket'})).toBeVisible()
		await expect(table.locator('tbody tr').filter({hasText: 'Doing task'})).toContainText('Doing')
		await expect(table.locator('tbody tr').filter({hasText: 'Backlog task'})).toContainText('To-Do')

		await page.getByRole('button', {name: 'Columns', exact: true}).click()
		await page.locator('.columns-filter').getByText('Bucket', {exact: true}).click()
		await expect(table.locator('th').filter({hasText: 'Bucket'})).toHaveCount(0)

		await page.reload()

		await expect(table.locator('tbody tr').filter({hasText: 'Doing task'})).toBeVisible()
		await expect(table.locator('th').filter({hasText: 'Bucket'})).toHaveCount(0)
	})

	test('Shows the new bucket in the list after moving the task on the kanban board', async ({
		authenticatedPage: page,
	}) => {
		await seedBuckets()
		await page.goto('/projects/1/1')
		const tasks = page.locator('.list-view .single-task')
		await expect(tasks.filter({hasText: 'Doing task'}).locator('.task-bucket')).toHaveText('Doing')

		await page.locator('.switch-view').getByText('Kanban', {exact: true}).click()
		const card = page.locator('.kanban .bucket .tasks .task').filter({hasText: 'Doing task'})
		const move = page.waitForResponse(response =>
			response.url().includes('/buckets/3/tasks') && response.request().method() !== 'GET',
		)
		await card.dragTo(page.locator('.kanban .bucket:nth-child(3) .tasks'))
		await move

		await page.locator('.switch-view').getByText('List', {exact: true}).click()

		await expect(tasks.filter({hasText: 'Doing task'}).locator('.task-bucket')).toHaveText('Done')

		await page.reload()

		await expect(tasks.filter({hasText: 'Doing task'}).locator('.task-bucket')).toHaveText('Done')
	})

	test('Moves the badge to the done bucket when a task is ticked in the list and back when unticked', async ({
		authenticatedPage: page,
	}) => {
		await seedBuckets({doneBucket: true})
		await page.goto('/projects/1/1')
		const row = page.locator('.list-view .single-task').filter({hasText: 'Doing task'})
		await expect(row.locator('.task-bucket')).toHaveText('Doing')

		const tick = page.waitForResponse(response =>
			response.url().includes('/api/v2/tasks/1') && response.request().method() !== 'GET',
		)
		await row.locator('.fancy-checkbox').click()
		await tick

		await expect(row.locator('.task-bucket')).toHaveText('Done')

		await page.reload()

		await expect(row.locator('.task-bucket')).toHaveText('Done')

		const untick = page.waitForResponse(response =>
			response.url().includes('/api/v2/tasks/1') && response.request().method() !== 'GET',
		)
		await row.locator('.fancy-checkbox').click()
		await untick

		await expect(row.locator('.task-bucket')).toHaveText('To-Do')

		await page.reload()

		await expect(row.locator('.task-bucket')).toHaveText('To-Do')
	})
})
