import type {
	APIRequestContext,
	Browser,
	Page,
} from '@playwright/test'
import {test, expect} from '../../support/fixtures'
import {login} from '../../support/authenticateUser'
import {BucketFactory} from '../../factories/bucket'
import {TaskBucketFactory} from '../../factories/task_buckets'
import {TaskFactory} from '../../factories/task'
import {UserFactory} from '../../factories/user'
import {UserProjectFactory} from '../../factories/users_project'
import {createProjects} from '../project/prepareProjects'

const BOARD = '/projects/1/4'
const LIST = '/projects/1/1'
const KANBAN_VIEW = 4
const BUCKET = 1

// 第二个上下文的 WebSocket 必须先订阅好，否则改动可能早于订阅到达。
function waitForSubscribed(page: Page) {
	return page.waitForEvent('websocket').then(socket => socket.waitForEvent('framesent', {
		predicate: frame => typeof frame.payload === 'string' && frame.payload.includes('project.changed'),
	}))
}

async function seedProject(taskCount: number) {
	await createProjects(1)
	await BucketFactory.create(1, {
		id: BUCKET,
		project_view_id: KANBAN_VIEW,
		position: 100,
	})
	const tasks = await TaskFactory.create(taskCount, {
		id: (i: number) => i,
		project_id: 1,
		title: (i: number) => `Task number ${i}`,
	})
	for (const task of tasks) {
		await TaskBucketFactory.create(1, {
			task_id: task.id,
			bucket_id: BUCKET,
			project_view_id: KANBAN_VIEW,
		}, false)
	}
	return tasks
}

async function openReaderContext(browser: Browser, apiContext: APIRequestContext) {
	const [reader] = await UserFactory.create(1, {id: 100}, false)
	await UserProjectFactory.create(1, {
		id: 1,
		project_id: 1,
		user_id: reader.id,
		permission: 0,
	}, false)
	const context = await browser.newContext()
	const page = await context.newPage()
	await login(page, apiContext, reader)
	return {
		context,
		page,
	}
}

async function renameFirstTask(page: Page, title: string) {
	await page.goto('/tasks/1')
	const heading = page.locator('.task-view h1[contenteditable]')
	await heading.fill(title)
	await heading.press('Enter')
	await expect(heading).toHaveText(title)
}

test('renaming a task in one context updates the board of another', async ({authenticatedPage: page, apiContext, browser}) => {
	await seedProject(1)
	const reader = await openReaderContext(browser, apiContext)
	try {
		const subscribed = waitForSubscribed(reader.page)
		await reader.page.goto(BOARD)
		await subscribed
		await expect(reader.page.locator('.kanban .bucket .tasks .task').filter({hasText: 'Task number 1'})).toBeVisible()

		await renameFirstTask(page, 'Renamed from elsewhere')

		await expect(reader.page.locator('.kanban .bucket .tasks .task').filter({hasText: 'Renamed from elsewhere'})).toBeVisible()
		await expect(reader.page.locator('.kanban .bucket .tasks .task').filter({hasText: 'Task number 1'})).toHaveCount(0)
	} finally {
		await reader.context.close()
	}
})

test('renaming a task in one context updates the list of another', async ({authenticatedPage: page, apiContext, browser}) => {
	await seedProject(1)
	const reader = await openReaderContext(browser, apiContext)
	try {
		const subscribed = waitForSubscribed(reader.page)
		await reader.page.goto(LIST)
		await subscribed
		await expect(reader.page.locator('.tasks .task').filter({hasText: 'Task number 1'})).toBeVisible()

		await renameFirstTask(page, 'Renamed from elsewhere')

		await expect(reader.page.locator('.tasks .task').filter({hasText: 'Renamed from elsewhere'})).toBeVisible()
		await expect(reader.page.locator('.tasks .task').filter({hasText: 'Task number 1'})).toHaveCount(0)
	} finally {
		await reader.context.close()
	}
})

test('a column that scrolled to its second page keeps it when another context changes a task', async ({authenticatedPage: page, apiContext, browser}) => {
	await seedProject(30)
	const reader = await openReaderContext(browser, apiContext)
	try {
		await reader.page.setViewportSize({
			width: 1280,
			height: 500,
		})
		const subscribed = waitForSubscribed(reader.page)
		await reader.page.goto(BOARD)
		await subscribed

		const cards = reader.page.locator('.kanban .bucket .tasks .task')
		await expect(cards).toHaveCount(25)

		await reader.page.locator('.kanban .bucket .tasks').first().evaluate(element => {
			element.scrollTop = element.scrollHeight
		})
		await expect(cards).toHaveCount(30)

		await renameFirstTask(page, 'Renamed from elsewhere')

		await expect(reader.page.locator('.kanban .bucket .tasks .task').filter({hasText: 'Renamed from elsewhere'})).toBeVisible()
		// 以前重新拉取看板只请求第一页，第二页的卡片会被丢掉。
		await expect(cards).toHaveCount(30)
	} finally {
		await reader.context.close()
	}
})

test('creating a bucket in one context adds the column to the board of another', async ({authenticatedPage: page, apiContext, browser}) => {
	await seedProject(1)
	const reader = await openReaderContext(browser, apiContext)
	try {
		const subscribed = waitForSubscribed(reader.page)
		await reader.page.goto(BOARD)
		await subscribed
		await expect(reader.page.locator('.kanban .bucket .title').filter({hasText: 'Next up'})).toHaveCount(0)

		await page.goto(BOARD)
		await page.locator('.kanban .bucket.new-bucket .button').click()
		await page.locator('.kanban .bucket.new-bucket input.input').fill('Next up')
		await page.locator('.kanban .bucket.new-bucket input.input').press('Enter')
		await expect(page.locator('.kanban .bucket .title').filter({hasText: 'Next up'})).toBeVisible()

		await expect(reader.page.locator('.kanban .bucket .title').filter({hasText: 'Next up'})).toBeVisible()
	} finally {
		await reader.context.close()
	}
})

test('dragging a card in one context moves it on the board of another', async ({authenticatedPage: page, apiContext, browser}) => {
	await seedProject(1)
	await BucketFactory.create(1, {
		id: 2,
		project_view_id: KANBAN_VIEW,
		position: 200,
	}, false)
	const reader = await openReaderContext(browser, apiContext)
	try {
		const subscribed = waitForSubscribed(reader.page)
		await reader.page.goto(BOARD)
		await subscribed
		await expect(reader.page.locator('.kanban .bucket:nth-child(1) .tasks')).toContainText('Task number 1')

		await page.goto(BOARD)
		await page.locator('.kanban .bucket .tasks .task').filter({hasText: 'Task number 1'}).dragTo(
			page.locator('.kanban .bucket:nth-child(2) .tasks'),
		)

		await expect(reader.page.locator('.kanban .bucket:nth-child(2) .tasks')).toContainText('Task number 1')
		await expect(reader.page.locator('.kanban .bucket:nth-child(1) .tasks')).not.toContainText('Task number 1')
	} finally {
		await reader.context.close()
	}
})

test('trashing the project in another context removes it from the sidebar', async ({apiContext, browser, userToken}) => {
	await seedProject(1)
	const reader = await openReaderContext(browser, apiContext)
	try {
		const subscribed = waitForSubscribed(reader.page)
		await reader.page.goto(BOARD)
		await subscribed
		const sidebarProject = reader.page.locator('li[data-project-id="1"]')
		await expect(sidebarProject).toBeVisible()

		// 直接共享的读用户也要收到项目进回收站的推送，所以这里用 API 而不是页面删除。
		const response = await apiContext.delete('/api/v1/projects/1', {
			headers: {Authorization: `Bearer ${userToken}`},
		})
		expect(response.ok()).toBe(true)

		await expect(sidebarProject).toHaveCount(0)
	} finally {
		await reader.context.close()
	}
})
