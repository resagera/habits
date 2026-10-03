import { computed, ref } from 'vue'
import { ApiError } from '../../../shared/api/client'
import { showToast } from '../../../shared/toast'
import * as tasksApi from '../api'
import type { Task, TaskCategory } from '../types'
import { currentTzOffset, isOverdue, todayISO } from '../types'

export type Tasks = ReturnType<typeof useTasks>

export function useTasks() {
  const categories = ref<TaskCategory[]>([])
  const tasks = ref<Task[]>([])
  const doneTasks = ref<Task[]>([])
  const loading = ref(true)
  const doneLoaded = ref(false)

  async function load() {
    try {
      const data = await tasksApi.fetchAll()
      categories.value = data.projects
      tasks.value = data.tasks
    } catch (e) {
      showToast(errorText(e))
    } finally {
      loading.value = false
    }
  }

  async function loadDone() {
    try {
      doneTasks.value = (await tasksApi.fetchDone()).tasks
      doneLoaded.value = true
    } catch (e) {
      showToast(errorText(e))
    }
  }

  function categoryOf(t: Task): TaskCategory | null {
    return categories.value.find((c) => c.id === t.project_id) ?? null
  }

  function replaceTask(next: Task) {
    const i = tasks.value.findIndex((t) => t.id === next.id)
    if (next.status_kind === 'open') {
      if (i >= 0) tasks.value[i] = next
      else {
        tasks.value.push(next)
        doneTasks.value = doneTasks.value.filter((t) => t.id !== next.id)
      }
    } else {
      if (i >= 0) tasks.value.splice(i, 1)
      const j = doneTasks.value.findIndex((t) => t.id === next.id)
      if (j >= 0) doneTasks.value[j] = next
      else doneTasks.value.unshift(next)
    }
  }

  async function quickAdd(title: string, categoryId: number | null): Promise<boolean> {
    try {
      const { task } = await tasksApi.createTask({
        title,
        project_id: categoryId ?? undefined,
        tz_offset_minutes: currentTzOffset(),
      })
      tasks.value.push(task)
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  /** Выполнить: оптимистично убираем из открытых; сервер может вернуть next-экземпляр. */
  async function complete(task: Task) {
    try {
      const { task: updated, next } = await tasksApi.completeTask(task.id)
      replaceTask(updated)
      if (next) {
        tasks.value.push(next)
        showToast(`Повтор: следующая задача ${next.due_date}`)
      }
    } catch (e) {
      showToast(errorText(e))
    }
  }

  async function patch(id: number, fields: Record<string, unknown>): Promise<Task | null> {
    try {
      const { task } = await tasksApi.updateTask(id, fields)
      replaceTask(task)
      return task
    } catch (e) {
      showToast(errorText(e))
      return null
    }
  }

  async function remove(id: number) {
    try {
      await tasksApi.deleteTask(id)
      tasks.value = tasks.value.filter((t) => t.id !== id)
      doneTasks.value = doneTasks.value.filter((t) => t.id !== id)
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  /** Поменять местами с соседом внутри группы (ручной порядок). */
  async function move(task: Task, dir: -1 | 1, group: Task[]) {
    const i = group.findIndex((t) => t.id === task.id)
    const j = i + dir
    if (i < 0 || j < 0 || j >= group.length) return
    const other = group[j]
    const a = task.position
    const b = other.position
    // одинаковые позиции (старые данные) — разводим детерминированно
    const posA = a === b ? b + dir : b
    const posB = a === b ? a : a
    await Promise.all([
      tasksApi.updateTask(task.id, { position: posA }).then(({ task: t }) => replaceTask(t)),
      tasksApi.updateTask(other.id, { position: posB }).then(({ task: t }) => replaceTask(t)),
    ]).catch((e) => showToast(errorText(e)))
  }

  // --- категории ---

  async function addCategory(name: string) {
    try {
      const { project } = await tasksApi.createCategory(name)
      categories.value.push(project)
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  function replaceCategory(c: TaskCategory) {
    const i = categories.value.findIndex((x) => x.id === c.id)
    if (i >= 0) categories.value[i] = c
  }

  async function removeCategory(id: number) {
    try {
      await tasksApi.deleteCategory(id)
      categories.value = categories.value.filter((c) => c.id !== id)
      // задачи категории упали во «Входящие» (или исчезли для участников)
      await load()
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  async function leaveCategory(id: number, myId: number) {
    try {
      await tasksApi.revokeCategoryShare(id, myId)
      await load()
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  // --- вычисления вкладок ---

  const byPriority = (a: Task, b: Task) =>
    b.priority - a.priority || a.position - b.position || a.id - b.id

  /** «Сегодня»: просроченные + срок сегодня, приоритет важнее. */
  const todayTasks = computed(() => {
    const today = todayISO()
    return tasks.value
      .filter((t) => t.due_date && t.due_date <= today)
      .sort((a, b) => (a.due_date! < b.due_date! ? -1 : a.due_date! > b.due_date! ? 1 : 0) || byPriority(a, b))
  })

  /** «Планы»: группы по датам (просроченные первой группой). */
  const plannedGroups = computed(() => {
    const today = todayISO()
    const groups = new Map<string, Task[]>()
    for (const t of tasks.value) {
      if (!t.due_date) continue
      const key = t.due_date < today ? '0-overdue' : t.due_date
      if (!groups.has(key)) groups.set(key, [])
      groups.get(key)!.push(t)
    }
    return [...groups.entries()]
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .map(([key, list]) => ({
        key,
        date: key === '0-overdue' ? '0000-00-00' : key,
        label: key === '0-overdue' ? 'Просрочено' : key,
        tasks: list.sort(byPriority),
      }))
  })

  return {
    categories,
    tasks,
    doneTasks,
    loading,
    doneLoaded,
    load,
    loadDone,
    categoryOf,
    replaceTask,
    replaceCategory,
    quickAdd,
    complete,
    patch,
    remove,
    move,
    addCategory,
    removeCategory,
    leaveCategory,
    todayTasks,
    plannedGroups,
    isOverdue,
  }
}

export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.message
  return 'Ошибка сети'
}
