// Общее состояние страницы настроек: строка поиска и свёрнутость секций.
//
// Ключи для provide/inject — страница владеет состоянием, а SettingsSection
// только читает: так секцию можно вставить где угодно, ничего не передавая
// пропсами через два уровня.
import type { InjectionKey, Ref } from 'vue'

export interface SectionDef {
  /** номер секции — он же ключ свёрнутости на сервере, менять нельзя */
  id: number
  title: string
  /** слова для поиска: то, чего нет в заголовке, но что могут искать */
  keywords: string
}

export interface CollapseApi {
  isOpen(id: number): boolean
  toggle(id: number): void
}

export const searchKey: InjectionKey<Ref<string>> = Symbol('settings-search')
export const collapseKey: InjectionKey<CollapseApi> = Symbol('settings-collapse')

export const SECTIONS = {
  profile: {
    id: 2,
    title: 'Профиль',
    keywords: 'кто я аккаунт устройства сеансы вход токен выйти id имя часовой пояс',
  },
  appearance: {
    id: 1, // был единственной сворачиваемой секцией до v2.100
    title: 'Оформление',
    keywords: 'тема цвета тёмная светлая фон картинка обои размытие затемнение автосмена',
  },
  headers: {
    id: 3,
    title: 'Заголовки страниц',
    keywords: 'шапка закрепить прокрутка меню',
  },
  pages: {
    id: 4,
    title: 'Какие страницы мне видны',
    keywords: 'скрыть страницы меню плитки видимость',
  },
  tokens: {
    id: 5,
    title: 'Токены доступа',
    keywords: 'вход браузер расширение веб-версия ключ',
  },
  notifications: {
    id: 8,
    title: 'Уведомления от бота',
    keywords: 'бот сообщения напоминания тихие часы ночь молчать выключить рассылка',
  },
  data: {
    id: 9,
    title: 'Мои данные',
    keywords: 'экспорт выгрузка скачать удалить аккаунт стереть',
  },
  transfer: {
    id: 7,
    title: 'Перенос настроек',
    keywords: 'экспорт импорт скопировать перенести сброс сбросить резервная копия оформление',
  },
  about: {
    id: 6,
    title: 'О приложении',
    keywords: 'версия релиз пользователь',
  },
} satisfies Record<string, SectionDef>

/** Подходит ли секция под строку поиска (пустая строка — подходят все). */
export function matchSection(s: SectionDef, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return `${s.title} ${s.keywords}`.toLowerCase().includes(q)
}
