import {computed, type ComputedGetter} from 'vue'

import {useProjects} from '@/composables/useProjects'
import {useAuthStore} from '@/stores/auth'

export function shouldIncludeSubprojects(
	projectId: number,
	childProjects: readonly {is_archived: boolean}[],
): boolean {
	// The server leaves archived sub-projects out, so only live children make a project an aggregate.
	return projectId > 0 && childProjects.some(child => !child.is_archived)
}

// Whether a list or table view of this project also shows the tasks of its sub-projects.
// undefined while the project list that answers this is still loading.
export function useIncludeSubprojects(projectIdGetter: ComputedGetter<number>) {
	const projectList = useProjects()
	const authStore = useAuthStore()

	return computed(() => {
		const projectId = projectIdGetter()
		// Pseudo projects and link shares never aggregate, so there is nothing to wait for.
		if (projectId <= 0 || authStore.isLinkShareAuth) return false
		if (projectList.isLoading) return undefined
		return shouldIncludeSubprojects(projectId, projectList.getChildProjects(projectId))
	})
}
