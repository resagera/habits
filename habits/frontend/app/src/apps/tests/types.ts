// Страница «Тесты»: колоды вопросов с вариантами ответов и личным прогрессом.
// Первая колода — официальные экзаменационные вопросы по ПДД Армении.

export interface TestDeck {
  id: number
  slug: string
  title: string
  description: string
  lang: string
  source_url: string
  revision: string
  exam_size: number
  exam_minutes: number
  exam_allowed_mistakes: number
  total: number
  passed: number
  wrong: number
}

export interface TestGroup {
  id: number
  num: number
  title: string
  total: number
  passed: number
  wrong: number
}

export interface TestQuestion {
  id: number
  num: number
  group_id: number | null
  group_title: string
  text: string
  options: string[]
  image: string
  explanation: string
}

/** Пул вопросов прогона. */
export type TestScope = 'unpassed' | 'all' | 'wrong' | 'group'

export interface TestSession {
  id: number
  deck_id: number
  mode: 'study' | 'exam'
  scope: TestScope
  group_id: number | null
  total: number
  answered: number
  correct: number
  expires_at: string | null
  finished_at: string | null
  passed: boolean | null
  created_at: string
}

export interface AnswerResult {
  correct: boolean
  correct_idx: number
  status: 'wrong' | 'passed'
  session: TestSession
  next?: TestQuestion
  position?: number
}

export interface ReviewItem {
  question: TestQuestion
  correct_idx: number
  chosen_idx: number | null
  is_correct: boolean | null
}

/** Картинки вопросов лежат в DATA_DIR/tests и раздаются из /uploads/tests. */
export function questionImageUrl(name: string): string {
  return name ? import.meta.env.BASE_URL + 'uploads/tests/' + name : ''
}
