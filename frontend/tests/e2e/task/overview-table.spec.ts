import {test, expect} from '../../support/fixtures'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {createDefaultViews} from '../project/prepareProjects'

const DAY = 24 * 60 * 60 * 1000

async function seedOverview() {
	const project = (await ProjectFactory.create(1, {id: 1, title: 'Overview Table Project'}))[0]
	await createDefaultViews(project.id)
	const dueSoon = new Date(Date.now() + DAY).toISOString()
	const overdue = new Date(Date.now() - 3 * DAY).toISOString()
	await TaskFactory.create(1, {id: 1, project_id: project.id, title: 'Open task', due_date: dueSoon}, false)
	await TaskFactory.create(1, {id: 2, project_id: project.id, title: 'Done task', due_date: dueSoon, done: true}, false)
	await TaskFactory.create(1, {id: 3, project_id: project.id, title: 'Overdue task', due_date: overdue}, false)
	return project
}

test.describe('Overview view mode', () => {
	test('Shows the list by default', async ({authenticatedPage: page}) => {
		await seedOverview()

		await page.goto('/')

		const overview = page.locator('[data-cy="showTasks"]')
		await expect(overview.locator('.card .task').filter({hasText: 'Open task'})).toBeVisible()
		await expect(overview.locator('table.table')).toHaveCount(0)
		await expect(overview.getByRole('button', {name: 'List', exact: true})).toHaveAttribute('aria-pressed', 'true')
	})

	test('Switches to a table with a Project column and remembers the choice', async ({authenticatedPage: page}) => {
		await seedOverview()
		await page.goto('/')
		const overview = page.locator('[data-cy="showTasks"]')

		await overview.getByRole('button', {name: 'Table', exact: true}).click()

		const table = overview.locator('table.table')
		await expect(table.locator('th').filter({hasText: 'Project'})).toBeVisible()
		const row = table.locator('tbody tr').filter({hasText: 'Open task'})
		await expect(row).toContainText('Overview Table Project')
		await expect(overview.locator('.tasks')).toHaveCount(0)

		await page.reload()

		await expect(overview.locator('table.table tbody tr').filter({hasText: 'Open task'})).toBeVisible()
		await expect(overview.getByRole('button', {name: 'Table', exact: true})).toHaveAttribute('aria-pressed', 'true')

		await overview.getByRole('button', {name: 'List', exact: true}).click()

		await expect(overview.locator('table.table')).toHaveCount(0)
		await expect(overview.locator('.card .task').filter({hasText: 'Open task'})).toBeVisible()
	})

	test('Remembers the column choice of the table', async ({authenticatedPage: page}) => {
		await seedOverview()
		await page.goto('/')
		const overview = page.locator('[data-cy="showTasks"]')
		await overview.getByRole('button', {name: 'Table', exact: true}).click()
		const projectHeader = overview.locator('table.table th').filter({hasText: 'Project'})
		await expect(projectHeader).toBeVisible()

		await overview.getByRole('button', {name: 'Columns', exact: true}).click()
		await page.locator('.columns-filter').getByText('Project', {exact: true}).click()
		await expect(projectHeader).toHaveCount(0)

		await page.reload()

		await expect(overview.locator('table.table tbody tr').filter({hasText: 'Open task'})).toBeVisible()
		await expect(projectHeader).toHaveCount(0)
	})

	test('Keeps the filters of the page when switching', async ({authenticatedPage: page}) => {
		await seedOverview()
		await page.goto('/tasks/by/upcoming')
		const overview = page.locator('[data-cy="showTasks"]')
		const tasks = overview.locator('.card .task')
		await expect(tasks.filter({hasText: 'Open task'})).toBeVisible()
		await expect(tasks.filter({hasText: 'Done task'})).toHaveCount(0)
		await expect(tasks.filter({hasText: 'Overdue task'})).toHaveCount(0)

		await overview.getByRole('button', {name: 'Table', exact: true}).click()

		const rows = overview.locator('table.table tbody tr')
		await expect(rows).toHaveCount(1)
		await expect(rows).toContainText('Open task')

		await overview.getByText('Show overdue tasks').click()

		await expect(page).toHaveURL(/showOverdue=true/)
		await expect(rows).toHaveCount(2)
		await expect(rows.filter({hasText: 'Overdue task'})).toBeVisible()
		await expect(rows.filter({hasText: 'Done task'})).toHaveCount(0)
	})

	test('Sorts the table by the clicked column', async ({authenticatedPage: page}) => {
		await seedOverview()
		await page.goto('/tasks/by/upcoming?showOverdue=true')
		const overview = page.locator('[data-cy="showTasks"]')
		await overview.getByRole('button', {name: 'Table', exact: true}).click()
		const rows = overview.locator('table.table tbody tr')

		// Without a chosen sort the table keeps the order of the list: the overdue task comes first.
		await expect(rows.first()).toContainText('Overdue task')

		await overview.getByRole('button', {name: 'Sort by Due Date'}).click()

		await expect(rows.first()).toContainText('Open task')
	})
})
