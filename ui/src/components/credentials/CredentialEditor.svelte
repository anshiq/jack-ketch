<script lang="ts">
  import { untrack } from 'svelte';
  import Plus from '@lucide/svelte/icons/plus';
  import X from '@lucide/svelte/icons/x';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Info from '@lucide/svelte/icons/info';
  import { CredentialService } from '../../lib/api';
  import type { CredentialDraft, CredentialKind, ExtractSource } from '../../lib/api/types';
  import {
    ENVIRONMENTS,
    KIND_OPTIONS,
    allowsCustomHeader,
    emptyRecipe,
    forcedSecret,
    headersToText,
    isProduction,
    looksSecret,
    newField,
    parseApps,
    textToHeaders,
    usesTarget,
    validateDraft
  } from '../../lib/credentialsView';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import Switch from '../../lib/ui/Switch.svelte';

  let {
    workspaceId,
    initial,
    knownApps = [],
    onClose,
    onSaved
  }: {
    workspaceId: string;
    initial: CredentialDraft;
    knownApps?: string[];
    onClose: () => void;
    onSaved: (name: string) => void;
  } = $props();

  const seed: CredentialDraft = untrack(() => JSON.parse(JSON.stringify(initial)));
  let draft = $state<CredentialDraft>(seed);
  let appsText = $state(seed.apps.join(', '));
  let recipeOn = $state(seed.recipe !== null);
  let headersText = $state(headersToText(seed.recipe?.headers ?? {}));
  let submitted = $state(false);
  let saving = $state(false);
  let error = $state('');

  const isNew = seed.id === '';
  const issues = $derived(validateDraft({ ...draft, recipe: recipeOn ? (draft.recipe ?? emptyRecipe()) : null }));
  const prod = $derived(isProduction(draft.environment));
  const SOURCES: { value: ExtractSource; label: string }[] = [
    { value: 'json', label: 'JSON body' },
    { value: 'header', label: 'Response header' },
    { value: 'cookie', label: 'Response cookie' },
    { value: 'regex', label: 'Regex on body' }
  ];

  function toggleRecipe(on: boolean) {
    recipeOn = on;
    if (on && !draft.recipe) {
      draft.recipe = emptyRecipe();
      headersText = '';
    }
  }

  function addField(kind: CredentialKind) {
    const f = newField(kind);
    const taken = new Set(draft.fields.map((x) => x.name.toLowerCase()));
    let name = f.name;
    let i = 2;
    while (taken.has(name.toLowerCase())) name = `${f.name}_${i++}`;
    f.name = name;
    draft.fields.push(f);
  }

  function removeField(i: number) {
    draft.fields.splice(i, 1);
  }

  function changeKind(i: number, kind: CredentialKind) {
    const f = draft.fields[i];
    if (!f) return;
    f.kind = kind;
    if (forcedSecret(kind)) f.secret = true;
    if (!usesTarget(kind) && !allowsCustomHeader(kind)) f.target = '';
    if (usesTarget(kind) && !f.target) f.target = newField(kind).target;
  }

  function onNameInput(i: number) {
    const f = draft.fields[i];
    if (f && looksSecret(f)) f.secret = true;
  }

  function onValueInput(i: number) {
    const f = draft.fields[i];
    if (f && f.value !== '') f.keep = false;
  }

  async function save() {
    submitted = true;
    error = '';
    if (!issues.ok) return;
    saving = true;
    try {
      const recipe = recipeOn && draft.recipe ? { ...draft.recipe, headers: textToHeaders(headersText) } : null;
      const saved = await CredentialService.upsert(workspaceId, { ...draft, apps: parseApps(appsText), recipe });
      toasts.ok(isNew ? `Added credential profile ${saved.name}` : `Saved ${saved.name}`);
      onSaved(saved.name);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      toastError(err);
    } finally {
      saving = false;
    }
  }
</script>

<Dialog title={isNew ? 'New credential profile' : `Edit ${seed.name}`} description="One identity the agent can use for authenticated calls. Secret values are encrypted and never shown to the agent." icon={KeyRound} width={780} {onClose}>
  {#snippet children()}
    <form
      class="editor"
      onsubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <div class="row2">
        <div class="form-field">
          <label for="cp-name">Name</label>
          <input id="cp-name" class="mono" bind:value={draft.name} placeholder="admin-tenant-a" autocomplete="off" spellcheck="false" aria-invalid={submitted && !!issues.name} />
          {#if submitted && issues.name}<span class="err-text">{issues.name}</span>{:else}<span class="hint">The agent refers to the profile by this name.</span>{/if}
        </div>
        <div class="form-field">
          <label for="cp-env">Environment</label>
          <input id="cp-env" list="cp-envs" bind:value={draft.environment} placeholder="test" autocomplete="off" />
          <datalist id="cp-envs">{#each ENVIRONMENTS as e (e)}<option value={e}></option>{/each}</datalist>
          <span class="hint">{prod ? 'Production profiles ask for approval on every request.' : 'Use test or staging accounts, not real users.'}</span>
        </div>
      </div>

      <div class="form-field">
        <label for="cp-desc">Description <span class="opt">optional</span></label>
        <input id="cp-desc" bind:value={draft.description} placeholder="Tenant admin with all permissions" autocomplete="off" />
      </div>

      <div class="row2">
        <div class="form-field">
          <label for="cp-apps">Only for apps <span class="opt">optional</span></label>
          <input id="cp-apps" class="mono" bind:value={appsText} list="cp-app-list" placeholder="all apps" autocomplete="off" spellcheck="false" />
          <datalist id="cp-app-list">{#each knownApps as a (a)}<option value={a}></option>{/each}</datalist>
          <span class="hint">Comma separated app names. Empty means every app in the project.</span>
        </div>
        <div class="form-field">
          <span class="field-label">Default profile</span>
          <div class="switch-row">
            <Switch bind:checked={draft.default} label="Use as the project default" />
            <span class="hint">The agent attaches the default automatically when it does not name a profile.</span>
          </div>
        </div>
      </div>

      {#if prod}
        <div class="banner warn"><TriangleAlert size={14} /><span>This profile is tagged <strong>{draft.environment}</strong>. Anything that uses it can reach real data; keep it out of automated checks.</span></div>
      {/if}

      <section class="block">
        <div class="block-head">
          <h4>Fields</h4>
          <div class="adders" role="group" aria-label="Add a field">
            {#each KIND_OPTIONS as k (k.value)}
              <button type="button" class="btn ghost sm" title={k.hint} onclick={() => addField(k.value)}><Plus size={12} />{k.label}</button>
            {/each}
          </div>
        </div>
        {#if draft.fields.length === 0}
          <p class="muted small">No fields yet. Add a bearer token, cookie, tenant header or API key.</p>
        {/if}
        <div class="fields">
          {#each draft.fields as f, i (i)}
            <div class="frow" class:bad={submitted && !!issues.fields[i]}>
              <input class="mono name" bind:value={f.name} oninput={() => onNameInput(i)} placeholder="name" aria-label="Field name {i + 1}" autocomplete="off" spellcheck="false" />
              <select value={f.kind} aria-label="Field kind {i + 1}" onchange={(e) => changeKind(i, (e.currentTarget as HTMLSelectElement).value as CredentialKind)}>
                {#each KIND_OPTIONS as k (k.value)}<option value={k.value}>{k.label}</option>{/each}
              </select>
              {#if usesTarget(f.kind) || allowsCustomHeader(f.kind)}
                <input
                  class="mono target"
                  bind:value={f.target}
                  oninput={() => onNameInput(i)}
                  placeholder={f.kind === 'cookie' ? 'cookie name' : f.kind === 'query' ? 'parameter' : f.kind === 'header' ? 'Header-Name' : 'Authorization'}
                  aria-label="Target {i + 1}"
                  autocomplete="off"
                  spellcheck="false"
                />
              {:else}
                <span class="target none muted">{f.kind === 'variable' ? '${cred:' + f.name + '}' : 'Authorization'}</span>
              {/if}
              <input
                class="mono value"
                type={f.secret ? 'password' : 'text'}
                bind:value={f.value}
                oninput={() => onValueInput(i)}
                placeholder={f.keep ? '•••••••• unchanged, type to replace' : f.kind === 'basic' ? 'user:password' : 'value'}
                aria-label="Value {i + 1}"
                autocomplete="off"
                spellcheck="false"
                data-lpignore="true"
                data-1p-ignore="true"
              />
              <label class="sec" title={looksSecret(f) ? 'Always secret: bearer, basic and cookie values, and names that look like a key, token, secret, password or session' : 'Secret values are encrypted and hidden from the agent'}>
                <input type="checkbox" bind:checked={f.secret} disabled={looksSecret(f)} />
                <span>secret</span>
              </label>
              <button type="button" class="btn ghost icon sm" aria-label="Remove field {i + 1}" title="Remove" onclick={() => removeField(i)}><X size={14} /></button>
              {#if submitted && issues.fields[i]}<span class="err-text full">{issues.fields[i]}</span>{/if}
            </div>
          {/each}
        </div>
        <p class="hint">
          Non-secret fields such as a tenant id are visible to the agent so it can build URLs. Use
          <span class="mono">{'${cred:name}'}</span> in a probe URL, header or body to insert any field.
        </p>
      </section>

      <section class="block">
        <div class="block-head">
          <h4>Login recipe <span class="opt">optional</span></h4>
          <Switch checked={recipeOn} label="Fetch and refresh the token with a login request" onchange={(e) => toggleRecipe((e.currentTarget as HTMLInputElement).checked)} />
        </div>
        {#if recipeOn && draft.recipe}
          <p class="hint">jack-ketch sends this request when the token is missing, expired or rejected (HTTP 401), stores what it extracts, and retries once.</p>
          <div class="recipe">
            <div class="line">
              <select bind:value={draft.recipe.method} aria-label="Login method">
                {#each ['POST', 'GET', 'PUT', 'PATCH'] as m (m)}<option value={m}>{m}</option>{/each}
              </select>
              <input class="mono grow" bind:value={draft.recipe.url} placeholder="/auth/login" aria-label="Login URL" autocomplete="off" spellcheck="false" />
            </div>
            {#if submitted && issues.recipe}<span class="err-text">{issues.recipe}</span>{/if}
            <div class="row2">
              <div class="form-field">
                <label for="cp-rh">Headers <span class="opt">Name: value per line</span></label>
                <textarea id="cp-rh" class="mono" rows="3" bind:value={headersText} placeholder="Content-Type: application/json"></textarea>
              </div>
              <div class="form-field">
                <label for="cp-rb">Body</label>
                <textarea id="cp-rb" class="mono" rows="3" bind:value={draft.recipe.body} spellcheck="false"></textarea>
              </div>
            </div>
            <div class="extracts">
              <div class="block-head small">
                <h5>Take from the response</h5>
                <button type="button" class="btn ghost sm" onclick={() => draft.recipe?.extract.push({ source: 'json', path: '', field: '', kind: 'bearer', target: '', expiresInPath: '' })}><Plus size={12} />Add rule</button>
              </div>
              {#each draft.recipe.extract as e, i (i)}
                <div class="erow">
                  <select bind:value={e.source} aria-label="Source {i + 1}">
                    {#each SOURCES as s (s.value)}<option value={s.value}>{s.label}</option>{/each}
                  </select>
                  <input class="mono" bind:value={e.path} placeholder={e.source === 'json' ? 'data.access_token' : e.source === 'regex' ? '"token":"([^"]+)"' : e.source === 'header' ? 'X-Auth-Token' : 'session'} aria-label="Path {i + 1}" spellcheck="false" />
                  <span class="arrow">→</span>
                  <input class="mono" bind:value={e.field} placeholder="token" aria-label="Field {i + 1}" spellcheck="false" />
                  <select bind:value={e.kind} aria-label="Field kind for rule {i + 1}" title="Kind used when the field does not exist yet">
                    {#each KIND_OPTIONS as k (k.value)}<option value={k.value}>{k.label}</option>{/each}
                  </select>
                  {#if e.source === 'json'}
                    <input class="mono" bind:value={e.expiresInPath} placeholder="expires_in (seconds)" aria-label="Expiry path {i + 1}" spellcheck="false" />
                  {:else}
                    <span></span>
                  {/if}
                  <button type="button" class="btn ghost icon sm" aria-label="Remove rule {i + 1}" onclick={() => draft.recipe?.extract.splice(i, 1)}><X size={14} /></button>
                  {#if submitted && issues.extract[i]}<span class="err-text full">{issues.extract[i]}</span>{/if}
                </div>
              {/each}
              <div class="ttl">
                <label for="cp-ttl">Assume the token lives for <span class="opt">seconds, when the response has no expiry</span></label>
                <input id="cp-ttl" class="mono" inputmode="numeric" value={draft.recipe.ttlSeconds || ''} oninput={(e) => draft.recipe && (draft.recipe.ttlSeconds = Number((e.currentTarget as HTMLInputElement).value) || 0)} placeholder="3600" />
              </div>
            </div>
          </div>
        {/if}
      </section>

      <div class="form-field">
        <span class="field-label">Cookie jar</span>
        <div class="switch-row">
          <Switch bind:checked={draft.captureCookies} label="Keep cookies the app sets" />
          <span class="hint">Set-Cookie responses to authenticated requests are stored (encrypted) and sent back, so rotating sessions keep working.</span>
        </div>
      </div>

      <div class="banner info"><Info size={14} /><span>Secret values stay in the daemon. The agent only sees names, kinds and token status, and every value is scrubbed from probe results and logs.</span></div>

      {#if error}<div class="banner err"><TriangleAlert size={14} /><span>{error}</span></div>{/if}
      <button type="submit" class="hidden" tabindex="-1" aria-hidden="true"></button>
    </form>
  {/snippet}
  {#snippet footer()}
    <button type="button" class="btn" onclick={onClose}>Cancel</button>
    <button type="button" class="btn primary" disabled={saving} onclick={() => void save()}>
      {#if saving}<Spinner size={14} />{/if}{isNew ? 'Add profile' : 'Save'}
    </button>
  {/snippet}
</Dialog>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .row2 {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--space-4);
  }
  .field-label {
    font-size: var(--fs-xs);
    color: var(--text-1);
    font-weight: 500;
  }
  .opt {
    color: var(--text-2);
    font-weight: 400;
    font-size: var(--fs-xs);
  }
  .switch-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-1);
  }
  .block-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  .block-head h4,
  .block-head h5 {
    margin: 0;
    font-size: var(--fs-sm);
    font-weight: 600;
    color: var(--text-0);
  }
  .block-head h5 {
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
  .adders {
    display: flex;
    gap: 2px;
    flex-wrap: wrap;
  }
  .fields {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .frow {
    display: grid;
    grid-template-columns: 120px 130px 130px minmax(0, 1fr) auto auto;
    gap: var(--space-2);
    align-items: center;
  }
  .frow.bad input,
  .frow.bad select {
    border-color: var(--err);
  }
  .frow input[type='text'],
  .frow input[type='password'],
  .frow select,
  .erow input,
  .erow select,
  .line select,
  .line input,
  textarea,
  .ttl input,
  #cp-name,
  #cp-env,
  #cp-desc,
  #cp-apps {
    height: 30px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 0 8px;
    font-size: var(--fs-sm);
    min-width: 0;
  }
  textarea {
    height: auto;
    padding: 6px 8px;
    resize: vertical;
    font-size: var(--fs-xs);
  }
  .frow input:focus,
  .frow select:focus,
  .erow input:focus,
  .erow select:focus,
  .line select:focus,
  .line input:focus,
  textarea:focus,
  .ttl input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .target.none {
    font-size: var(--fs-xs);
    padding: 0 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sec {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--fs-xs);
    color: var(--text-2);
    white-space: nowrap;
  }
  .full {
    grid-column: 1 / -1;
  }
  .recipe {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .line {
    display: flex;
    gap: var(--space-2);
  }
  .grow {
    flex: 1;
  }
  .extracts {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .erow {
    display: grid;
    grid-template-columns: 130px minmax(0, 1.3fr) auto minmax(0, 0.8fr) 120px minmax(0, 1fr) auto;
    gap: var(--space-2);
    align-items: center;
  }
  .arrow {
    color: var(--text-2);
  }
  .ttl {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
  .ttl input {
    width: 100px;
  }
  .muted {
    color: var(--text-2);
  }
  .small {
    font-size: var(--fs-xs);
    margin: 0;
  }
  .hidden {
    display: none;
  }
  @media (max-width: 900px) {
    .row2 {
      grid-template-columns: minmax(0, 1fr);
    }
    .frow,
    .erow {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }
</style>
