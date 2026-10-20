import {computed, type ComputedGetter} from 'vue'

import {useProjects} from '@/composables/useProjects'

export function shouldIncludeSubprojects(
	projectId: number,
	childProjects: readonly {is_archived: boolean}[],
): boolean {
	// The server leaves archived sub-projects out, so only live children make a project an aggregate.
	return projectId > 0 && childProjects.some(child => !child.is_archived)
}

// Whether a list or table view of this project also shows the tasks of its sub-projects.
export function useIncludeSubprojects(projectIdGetter: ComputedGetter<number>) {
	const projectList = useProjects()

	return computed(() => shouldIncludeSubprojects(
		projectIdGetter(),
		projectList.getChildProjects(projectIdGetter()),
	))
}
