import { api } from '../../shared/api/client'
import type { Article, ArticleFolder, ArticleMeta, ArticleRevision, ContentHit, SharedTree } from './types'

export function fetchTree() {
  return api.get<{ folders: ArticleFolder[]; articles: ArticleMeta[]; shared: SharedTree[] }>('/articles/tree')
}

export function searchContent(q: string) {
  return api.get<{ hits: ContentHit[] }>(`/articles/search?q=${encodeURIComponent(q)}`)
}

// --- шаринг категории доступом (без копирования контента) ---

export function shareFolderTo(folderId: number, to: string) {
  return api.post<{ shared_to: { id: number; username: string; first_name: string } }>(
    `/articles/folders/${folderId}/share`,
    { to },
  )
}

export function fetchFolderShares(folderId: number) {
  return api.get<{ users: { id: number; username: string; first_name: string }[] }>(
    `/articles/folders/${folderId}/shares`,
  )
}

export function revokeFolderShare(folderId: number, userId: number) {
  return api.delete<void>(`/articles/folders/${folderId}/shares/${userId}`)
}

/** Получатель убирает доступную категорию у себя. */
export function leaveShared(folderId: number) {
  return api.delete<void>(`/articles/shared/${folderId}`)
}

export function fetchArticle(id: number) {
  return api.get<{ article: Article; read_pos: number }>(`/articles/${id}`)
}

export function saveReadPos(id: number, pos: number) {
  return api.put<void>(`/articles/${id}/read-pos`, { pos })
}

export function fetchHistory(id: number) {
  return api.get<{ revisions: ArticleRevision[] }>(`/articles/${id}/history`)
}

export function fetchRevision(revId: number) {
  return api.get<{ content: string; saved_at: string }>(`/article-revisions/${revId}`)
}

export function readToken(id: number) {
  return api.post<{ token: string; path: string }>(`/articles/${id}/read-token`)
}

export function uploadArticleImage(file: File) {
  const form = new FormData()
  form.append('file', file)
  return api.upload<{ url: string }>('/articles/images', form)
}

export function createArticle(title: string, content: string, folderId: number | null) {
  return api.post<{ article: Article }>('/articles', { title, content, folder_id: folderId })
}

export function updateArticle(
  id: number,
  patch: { title?: string; content?: string; folder_id?: number | null; set_folder?: boolean },
) {
  return api.patch<{ article: Article }>(`/articles/${id}`, patch)
}

export function deleteArticle(id: number) {
  return api.delete<void>(`/articles/${id}`)
}

export function createFolder(name: string, parentId: number | null) {
  return api.post<{ folder: ArticleFolder }>('/articles/folders', { name, parent_id: parentId })
}

export function updateFolder(
  id: number,
  patch: { name?: string; parent_id?: number | null; set_parent?: boolean },
) {
  return api.patch<{ folder: ArticleFolder }>(`/articles/folders/${id}`, patch)
}

export function deleteFolder(id: number) {
  return api.delete<void>(`/articles/folders/${id}`)
}

export function shareToken(id: number) {
  return api.post<{ token: string; link: string }>(`/articles/${id}/share-token`)
}

export function downloadToken(id: number) {
  return api.post<{ token: string; path: string }>(`/articles/${id}/download-token`)
}

export function sendArticle(id: number, to: string) {
  return api.post<{ sent_to: { id: number; username: string; first_name: string } }>(
    `/articles/${id}/send`,
    { to },
  )
}
