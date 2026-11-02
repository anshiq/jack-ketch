import { monaco } from './monacoSetup';
import { SystemService } from './api';
import type { DirectoryEntry } from './api/types';
import { joinPathEntry, splitPathInput } from './pathSuggest';
import { yamlPathTarget } from './yamlPath';

let registered = false;

export function registerYamlPathCompletions(getBaseDir: () => string): void {
  if (registered) return;
  registered = true;
  monaco.languages.registerCompletionItemProvider('yaml', {
    triggerCharacters: ['/', '\\', '.'],
    provideCompletionItems: async (model, position) => {
      const line = model.getLineContent(position.lineNumber);
      const target = yamlPathTarget(line, position.column);
      if (!target) return { suggestions: [] };
      const { dir, prefix } = splitPathInput(target.value);
      let entries: DirectoryEntry[] = [];
      try {
        const res = await SystemService.listDirectory({ dir, prefix, baseDir: getBaseDir(), dirsOnly: target.dirsOnly, limit: 100 });
        entries = res.entries ?? [];
      } catch {
        return { suggestions: [] };
      }
      const range = new monaco.Range(position.lineNumber, target.startColumn, position.lineNumber, position.column);
      const suggestions = entries.map((entry, i) => {
        const insert = joinPathEntry(dir, entry.name, entry.isDir);
        return {
          label: entry.isDir ? `${entry.name}/` : entry.name,
          kind: entry.isDir ? monaco.languages.CompletionItemKind.Folder : monaco.languages.CompletionItemKind.File,
          detail: entry.isDir ? 'folder' : 'file',
          insertText: insert,
          filterText: entry.name,
          range,
          sortText: String(i).padStart(4, '0')
        };
      });
      return { suggestions, incomplete: false };
    }
  });
}
