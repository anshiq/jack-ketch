import { describe, expect, it } from 'vitest';
import { browserURL, countKinds, portKind, portMatches, shortCommand } from './portsView';
import type { PortEntry } from './api/services';

const managed: PortEntry = { port: 5173, pid: 10, listening: true, command: '/usr/bin/node vite', managed: { processId: 'p_1', app: 'web', status: 'running', workspaceId: 'w', workspacePath: '/src/app' } };
const conflict: PortEntry = { port: 3000, pid: 11, listening: true, command: 'python -m http.server 3000', declared: [{ workspaceId: 'w', workspacePath: '/src/app', app: 'api', name: 'http', port: 3000 }], conflict: 'declared by api/http but held by a process jack-ketch does not manage' };
const free: PortEntry = { port: 6379, listening: false, declared: [{ workspaceId: 'w', workspacePath: '/src/app', app: 'cache', name: 'redis', port: 6379 }] };

describe('portsView', () => {
  it('classifies ports', () => {
    expect(portKind(managed)).toBe('managed');
    expect(portKind(conflict)).toBe('conflict');
    expect(portKind(free)).toBe('declared');
    expect(portKind({ port: 22, listening: true, pid: 1 })).toBe('other');
    expect(countKinds([managed, conflict, free])).toMatchObject({ all: 3, managed: 1, conflict: 1, declared: 1 });
  });

  it('searches app, command and port', () => {
    expect(portMatches(managed, 'web')).toBe(true);
    expect(portMatches(conflict, '3000')).toBe(true);
    expect(portMatches(free, 'redis')).toBe(true);
    expect(portMatches(free, 'nope')).toBe(false);
  });

  it('shortens commands and builds browser urls', () => {
    expect(shortCommand('/usr/bin/node vite --port 5173')).toBe('node vite --port 5173');
    expect(browserURL({ port: 8080, listening: true, address: '*' })).toBe('http://localhost:8080/');
    expect(browserURL({ port: 8080, listening: true, address: '127.0.0.1' })).toBe('http://127.0.0.1:8080/');
  });
});
