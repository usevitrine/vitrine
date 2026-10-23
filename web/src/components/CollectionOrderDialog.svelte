<script lang="ts">
  // A collection's own order (the user: "I need to be able to manually order collections"): its comics as a list -
  // drag one by its grip (or arrow keys on it), A to Z puts them back by title, Save keeps it. Its page, its tile's
  // covers and the reader's Up next follow it. Opened from the collection's menu (collections.ordering).
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import { ListDrag } from '../lib/listDrag.svelte';
  import { api, type Comic } from '../lib/api';
  import { collections } from '../lib/collections.svelte';
  import { t } from '../lib/i18n.svelte';

  const k = $derived(collections.ordering);
  let list = $state<Comic[]>([]);
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');
  let changed = $state(false);

  $effect(() => {
    const c = k;
    if (!c) return;
    loading = true;
    error = '';
    changed = false;
    list = [];
    (async () => {
      try {
        const ids = (await api.search('', 1000, undefined, undefined, false, 'comics', c.id)).ids;
        const by = new Map((await api.comicInfo(ids)).comics.map((x) => [x.id, x]));
        if (collections.ordering === c) list = ids.map((id) => by.get(id)).filter((x) => x !== undefined);
      } catch (e) {
        error = (e as Error).message;
      } finally {
        loading = false;
      }
    })();
  });

  function close() {
    collections.ordering = null;
  }
  function move(from: number, to: number) {
    const next = [...list];
    const [x] = next.splice(from, 1);
    next.splice(to, 0, x);
    list = next;
    changed = true;
  }
  function aToZ() {
    const coll = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
    list = [...list].sort((a, b) => coll.compare(a.title, b.title) || a.id - b.id);
    changed = true;
  }
  async function save() {
    if (!k) return;
    saving = true;
    if (await collections.order(k, list.map((c) => c.id))) close();
    saving = false;
  }

  // ---- drag by the grip (as the saved searches' list)
  const lists = new ListDrag(move);
</script>

{#if k}
  <Modal title={t('collections.orderTitle', { name: k.name })} width={440} onclose={close}>
    {#if loading}
      <div class="wait" role="status" aria-label={t('common.loading')}><span class="spinner"></span></div>
    {:else if error}
      <p class="error">{error}</p>
    {:else}
      <ul class="list" class:dragging={!!lists.drag}>
        {#each list as c, i (c.id)}
          <li class:lifted={lists.drag?.from === i} style:transform={lists.shift(i) ? `translateY(${lists.shift(i)}px)` : undefined}>
            <button
              class="icon grip"
              aria-label={t('collections.moveComic', { title: c.title })}
              title={t('saved.dragToReorder')}
              onpointerdown={(e) => lists.start(e, i)}
              onpointermove={lists.moveTo}
              onpointerup={lists.end}
              onpointercancel={lists.end}
              onkeydown={(e) => {
                if (e.key === 'ArrowUp' && i > 0) move(i, i - 1);
                else if (e.key === 'ArrowDown' && i < list.length - 1) move(i, i + 1);
                else return;
                e.preventDefault();
              }}><Icon name="grip" size={18} /></button>
            <span class="num">{i + 1}</span>
            {#if c.thumb}<img src={c.thumb} alt="" draggable="false" />{:else}<span class="noimg"></span>{/if}
            <span class="title">{c.title}</span>
          </li>
        {/each}
      </ul>
    {/if}
    <div class="foot">
      <button type="button" class="quiet" onclick={aToZ} disabled={loading || list.length < 2}>{t('collections.aToZ')}</button>
      <span class="spacer"></span>
      <button type="button" class="quiet" onclick={close}>{t('common.cancel')}</button>
      <button type="button" onclick={save} disabled={!changed || saving}>{t('common.save')}</button>
    </div>
  </Modal>
{/if}

<style>
  .wait {
    display: grid;
    place-items: center;
    padding: 32px 0;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
    overflow-y: auto;
    overscroll-behavior: contain;
    min-height: 0;
  }
  li {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px 4px 2px;
    border-radius: 10px;
    background: var(--base);
    border: 1px solid var(--surface0);
    transition: transform 0.18s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .dragging li.lifted {
    transition: none;
    z-index: 1;
    position: relative;
    box-shadow: 0 8px 20px rgb(0 0 0 / 0.45);
    border-color: var(--mauve);
  }
  .grip {
    flex: none;
    cursor: grab;
    touch-action: none;
    color: var(--subtext);
  }
  .dragging .grip {
    cursor: grabbing;
  }
  .num {
    flex: none;
    min-width: 1.2em;
    margin-left: -6px;
    text-align: center;
    color: var(--subtext);
    font-size: var(--font-small);
    font-variant-numeric: tabular-nums;
  }
  img,
  .noimg {
    flex: none;
    width: 34px;
    height: 48px;
    object-fit: cover;
    border-radius: 5px;
    background: var(--surface0);
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }
  @media (prefers-reduced-motion: reduce) {
    li {
      transition: none;
    }
  }
</style>
