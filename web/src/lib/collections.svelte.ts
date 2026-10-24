// The comics' collections (comics that belong together - a series - named by the user): the list, and putting
// comics in one (or a new one), taking them out, renaming, taking one apart, deleting one with its comics. Each change says so in a notice and
// asks the Comics page to search again (a collection is one stacked tile there).
import { api, type Collection } from './api';
import { toasts } from './toast.svelte';
import { askText } from './prompt.svelte';
import { confirmAction } from './confirm.svelte';
import { itemName, t } from './i18n.svelte';

class Collections {
  list = $state<Collection[]>([]);
  /** The collection whose order is being set (CollectionOrderDialog), if any. */
  ordering = $state<Collection | null>(null);
  /** Called after a change (the Comics page searches again). */
  onchange: (() => void) | null = null;

  async load() {
    try {
      this.list = (await api.collections()).collections;
    } catch {
      /* the next time */
    }
  }

  private async done(text: string) {
    toasts.show(text, { cat: 'collections' });
    await this.load();
    this.onchange?.();
  }

  /** Puts comics in a collection - one of the list, or a new one (its name asked first). True once done. */
  async add(ids: number[], collection: number | 'new'): Promise<boolean> {
    try {
      if (collection === 'new') {
        const name = await askText({ title: t('collections.new'), label: t('collections.name'), placeholder: t('collections.namePlaceholder'), action: t('collections.create') });
        if (!name) return false;
        await api.createCollection(name, ids);
        await this.done(t('collections.created', { name, n: ids.length }));
        return true;
      }
      await api.setCollection(ids, collection);
      const k = this.list.find((x) => x.id === collection);
      await this.done(ids.length === 1 ? t('collections.addedOne', { what: itemName(ids[0], true), name: k?.name ?? '' }) : t('collections.added', { name: k?.name ?? '', n: ids.length }));
      return true;
    } catch (e) {
      toasts.error((e as Error).message);
      return false;
    }
  }

  async remove(ids: number[]): Promise<boolean> {
    try {
      await api.setCollection(ids, 0);
      await this.done(ids.length === 1 ? t('collections.removedOne', { what: itemName(ids[0], true) }) : t('collections.removed', { n: ids.length }));
      return true;
    } catch (e) {
      toasts.error((e as Error).message);
      return false;
    }
  }

  async rename(k: Collection) {
    const name = await askText({ title: t('collections.rename'), label: t('collections.name'), value: k.name, action: t('common.save') });
    if (!name || name === k.name) return;
    try {
      await api.renameCollection(k.id, name);
      await this.done(t('collections.renamed', { name }));
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  /** Puts a collection's comics in an order of the user's (its page, its tile's covers, Up next follow it). */
  async order(k: Collection, ids: number[]): Promise<boolean> {
    try {
      await api.orderCollection(k.id, ids);
      await this.done(t('collections.ordered', { name: k.name }));
      return true;
    } catch (e) {
      toasts.error((e as Error).message);
      return false;
    }
  }

  /** Takes a collection apart: its comics stay, on their own again. */
  async takeApart(k: Collection) {
    try {
      await api.deleteCollection(k.id);
      await this.done(t('collections.takenApart', { name: k.name }));
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  /** Deletes a collection with its comics (into the trash - Undo brings them, and it, back). */
  async deleteWithComics(k: Collection) {
    const ok = await confirmAction({
      title: t('collections.deleteTitle', { name: k.name }),
      body: t('collections.deleteBody', { n: k.count }),
      action: t('common.delete'),
      undoable: true,
    });
    if (!ok) return;
    try {
      const { ids } = await api.deleteCollectionComics(k.id);
      toasts.show(t('collections.deleted', { name: k.name }), {
        cat: 'deleted',
        kind: 'removed',
        action: {
          label: t('common.undo'),
          run: async () => {
            await api.undeleteComics(ids);
            await this.load();
            this.onchange?.();
          },
        },
      });
      await this.load();
      this.onchange?.();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
}

export const collections = new Collections();
