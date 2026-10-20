import {test, expect} from '../../support/fixtures'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {createDefaultViews} from './prepareProjects'

async function createProjectTree() {
	const parent = (await ProjectFactory.create(1, {id: 100, title: 'Parent Project'}))[0]
	const child = (await ProjectFactory.create(1, {
		id: 101,
		title: 'Child Project',
		parent_project_id: 100,
	}, false))[0]
	const grandchild = (await ProjectFactory.create(1, {
		id: 102,
		title: 'Grandchild Project',
		parent_project_id: 101,
	}, false))[0]

	const [parentList, , parentTable] = await createDefaultViews(parent.id, 100)
	await createDefaultViews(child.id, 104)
	const [, , grandchildTable] = await createDefaultViews(grandchild.id, 108)

	await TaskFactory.create(1, {id: 1, project_id: parent.id, title: 'Parent task'})
	await TaskFactory.create(1, {id: 2, project_id: child.id, title: 'Child task'}, false)
	await TaskFactory.create(1, {id: 3, project_id: grandchild.id, title: 'Grandchild task'}, false)

	return {parent, child, grandchild, parentList, parentTable, grandchildTable}
}

test.describe('Parent project with sub-projects', () => {
	test('List view shows the tasks of all sub-projects with their project name', async ({authenticatedPage: page}) => {
		const {parent, parentList} = await createProjectTree()

		await page.goto(`/projects/${parent.id}/${parentList.id}`)

		const rows = page.locator('.list-view .single-task')
		await expect(rows).toHaveCount(3)
		await expect(rows.filter({hasText: /Parent task/}).locator('.task-project')).toHaveText('Parent Project')
		await expect(rows.filter({hasText: /Child task/}).locator('.task-project')).toHaveText('Child Project')
		await expect(rows.filter({hasText: /Grandchild task/}).locator('.task-project')).toHaveText('Grandchild Project')
	})

	test('A sub-project task stays in the parent list after it is marked as done', async ({authenticatedPage: page}) => {
		const {parent, parentList} = await createProjectTree()
		await page.goto(`/projects/${parent.id}/${parentList.id}`)
		const rows = page.locator('.list-view .single-task')
		await expect(rows).toHaveCount(3)

		const childRow = rows.filter({hasText: /Child task/})
		const update = page.waitForResponse(response =>
			response.url().includes('/api/v2/tasks/2') && response.request().method() !== 'GET',
		)
		await childRow.locator('.fancy-checkbox').click()
		await update
		// The list is refetched after the update, so wait for that before asserting membership.
		await page.waitForLoadState('networkidle')

		await expect(rows).toHaveCount(3)
		await expect(childRow).toBeVisible()
	})

	test('List view of a sub-project only shows its own subtree', async ({authenticatedPage: page}) => {
		const {child} = await createProjectTree()

		await page.goto(`/projects/${child.id}`)

		const rows = page.locator('.list-view .single-task')
		await expect(rows).toHaveCount(2)
		await expect(rows.filter({hasText: /Parent task/})).toHaveCount(0)
	})

	test('Table view shows a Project column by default that can be switched off', async ({authenticatedPage: page}) => {
		const {parent, parentTable} = await createProjectTree()

		await page.goto(`/projects/${parent.id}/${parentTable.id}`)

		const table = page.locator('.project-table table.table')
		const projectHeader = table.locator('th').filter({hasText: 'Project'})
		await expect(projectHeader).toBeVisible()
		const grandchildRow = table.locator('tbody tr').filter({hasText: /Grandchild task/})
		await expect(grandchildRow).toContainText('Grandchild Project')
		await expect(table.locator('tbody tr')).toHaveCount(3)

		await page.getByRole('button', {name: 'Columns', exact: true}).click()
		await page.locator('.columns-filter').getByText('Project', {exact: true}).click()
		await expect(projectHeader).toHaveCount(0)

		await page.reload()
		await expect(table.locator('tbody tr')).toHaveCount(3)
		await expect(projectHeader).toHaveCount(0)
	})

	test('The Project column choice of a parent project does not change tables of other projects', async ({authenticatedPage: page}) => {
		const {grandchild, grandchildTable, parent, parentTable} = await createProjectTree()

		await page.goto(`/projects/${parent.id}/${parentTable.id}`)
		await page.getByRole('button', {name: 'Columns', exact: true}).click()
		await page.locator('.columns-filter').getByText('Project', {exact: true}).click()

		// The grandchild has no sub-projects, so its table keeps the default without a Project column.
		await page.goto(`/projects/${grandchild.id}/${grandchildTable.id}`)
		await expect(page.locator('.project-table tbody tr')).toHaveCount(1)
		await expect(page.locator('.project-table table.table th').filter({hasText: 'Project'})).toHaveCount(0)
	})
})
