import { IntegrationService } from '../api';

class HarnessNames {
  names = $state<Record<string, string>>({});
  loaded = $state(false);
  pending: Promise<void> | null = null;

  ensure(): Promise<void> {
    if (this.loaded) return Promise.resolve();
    if (this.pending) return this.pending;
    const p = (async () => {
      try {
        const list = await IntegrationService.listHarnesses();
        const names: Record<string, string> = {};
        for (const h of list) names[h.id] = h.displayName;
        this.names = names;
        this.loaded = true;
      } catch {
        return;
      } finally {
        this.pending = null;
      }
    })();
    this.pending = p;
    return p;
  }
}

export const harnessNames = new HarnessNames();
