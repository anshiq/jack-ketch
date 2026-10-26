import { SystemService } from '../api';
import { cache, mutation, query } from '../data/query.svelte';

export interface SettingsPayload {
  schema: Record<string, { type: string; enum?: string[]; description: string }>;
  settings: Record<string, unknown>;
}

export const settingsQuery = query<SettingsPayload>(
  () => 'settings',
  async () => {
    const res = await SystemService.getSettings();
    return {
      schema: (res.schema ?? {}) as SettingsPayload['schema'],
      settings: (res.settings ?? {}) as Record<string, unknown>
    };
  },
  { staleMs: 30000, refetchOnFocus: true, persist: true }
);

export const saveSettings = mutation(
  (payload: Record<string, unknown>) => SystemService.updateSettings(payload),
  {
    optimistic: (payload) => {
      const prev = cache.get<SettingsPayload>('settings');
      if (prev) {
        const settings: Record<string, unknown> = { ...prev.settings };
        for (const [k, v] of Object.entries(payload)) {
          const raw = typeof v === 'string' ? v.replace(/^"(.*)"$/, '$1') : v;
          settings[k] = raw;
        }
        cache.set('settings', { ...prev, settings });
      }
      return () => {
        if (prev) cache.set('settings', prev);
      };
    },
    onSuccess: (res) => {
      const prev = cache.get<SettingsPayload>('settings');
      const settings = (res.settings ?? prev?.settings ?? {}) as Record<string, unknown>;
      const clean: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(settings)) clean[k] = typeof v === 'string' ? v.replace(/^"(.*)"$/, '$1') : v;
      if (prev) cache.set('settings', { ...prev, settings: clean });
    }
  }
);
