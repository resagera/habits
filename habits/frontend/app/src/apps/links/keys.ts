import type { InjectionKey } from 'vue'
import type { LinkFolder, LinkItem } from './types'

export interface LinksHandlers {
  toggleFolder(id: number): void
  isOpen(id: number): boolean
  editFolder(folder: LinkFolder): void
  addLinkTo(folderId: number): void
  openLink(link: LinkItem): void
  copyLink(link: LinkItem): void
  shareLink(link: LinkItem): void
  editLink(link: LinkItem): void
}

export const linksHandlersKey: InjectionKey<LinksHandlers> = Symbol('links-handlers')
