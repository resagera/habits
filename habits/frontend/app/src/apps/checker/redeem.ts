// Приём контента по ссылке-приглашению t.me/<bot>?startapp=<prefix>_<token>:
// chk_ — шаблон чек-листа, art_ — статья. Telegram кладёт startapp в
// start_param (в браузере — ?tgWebAppStartParam=).
import { api } from '../../shared/api/client'
import { showToast } from '../../shared/toast'
import router from '../../router'

export async function redeemStartParam(): Promise<void> {
  const tg = (window as { Telegram?: { WebApp?: { initDataUnsafe?: { start_param?: string } } } }).Telegram
  const param =
    tg?.WebApp?.initDataUnsafe?.start_param ??
    new URLSearchParams(location.search).get('tgWebAppStartParam') ??
    ''
  let endpoint = ''
  let route = ''
  let label = ''
  if (param.startsWith('chk_')) {
    endpoint = '/checker/templates/redeem'
    route = '/checker'
    label = 'Шаблон'
  } else if (param.startsWith('chg_')) {
    endpoint = '/checker/groups/redeem'
    route = '/checker'
    label = 'Список'
  } else if (param.startsWith('rem_')) {
    endpoint = '/reminder-categories/redeem'
    route = '/reminders'
    label = 'Категория напоминаний'
  } else if (param.startsWith('art_')) {
    endpoint = '/articles/redeem'
    route = '/articles'
    label = 'Статья'
  } else if (param.startsWith('lnf_')) {
    endpoint = '/links/folders/redeem'
    route = '/links'
    label = 'Папка ссылок'
  } else if (param.startsWith('lnk_')) {
    endpoint = '/links/redeem'
    route = '/links'
    label = 'Ссылка'
  } else {
    return
  }
  // ссылка «прилипает» к чату — не добавляем повторно при каждом входе
  if (localStorage.getItem('redeemed_' + param)) return
  try {
    const data = await api.post<{
      template?: { name: string }
      group?: { name: string }
      category?: { name: string }
      article?: { title: string }
      folder?: { name: string }
      link?: { name: string }
    }>(endpoint, {
      token: param,
    })
    localStorage.setItem('redeemed_' + param, '1')
    const name =
      data.template?.name ??
      data.group?.name ??
      data.category?.name ??
      data.article?.title ??
      data.folder?.name ??
      data.link?.name ??
      ''
    showToast(`${label} «${name}» — добавлено 📥`)
    router.push(route)
  } catch {
    showToast('Приглашение не найдено или устарело')
  }
}
