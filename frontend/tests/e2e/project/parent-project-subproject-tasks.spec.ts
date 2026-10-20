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
	await createDefaultViews(grandchild.id, 108)

	await TaskFactory.create(1, {id: 1, project_id: parent.id, title: 'Parent task'})
	await TaskFactory.create(1, {id: 2, project_id: child.id, title: 'Child task'}, false)
	await TaskFactory.create(1, {id: 3, project_id: grandchild.id, title: 'Grandchild task'}, false)

	return {parent, child, grandchild, parentList, parentTable}
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

	test('List view of a sub-project only shows its own subtree', async ({authenticatedPage: page}) => {
		const {child} = await createProjectTree()

		await page.goto(`/projects/${child.id}`)

		const rows = page.locator('.list-view .single-task')
		await expect(rows).toHaveCount(2)
		await expect(rows.filter({hasText: /Parent task/})).toHaveCount(0)
	})

	test('Table view has a Project column that cannot be switched off', async ({authenticatedPage: page}) => {
		const {parent, parentTable} = await createProjectTree()

		await page.goto(`/projects/${parent.id}/${parentTable.id}`)

		const table = page.locator('.project-table table.table')
		await expect(table.locator('th').filter({hasText: 'Project'})).toBeVisible()
		const grandchildRow = table.locator('tbody tr').filter({hasText: 'Grandchild task'})
		await expect(grandchildRow).toContainText('Grandchild Project')
		await expect(table.locator('tbody tr')).toHaveCount(3)

		await page.getByRole('button', {name: 'Columns', exact: true}).click()
		await expect(page.locator('.columns-filter').getByText('Project', {exact: true})).toHaveCount(0)
	})
})
