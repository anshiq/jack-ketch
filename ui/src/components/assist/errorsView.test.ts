import { describe, expect, it } from 'vitest';
import { errorsDigest, kindInfo, locationLabel } from './errorsView';
import { toErrorsResult } from '../../lib/api/assist';

describe('errorsView', () => {
  it('maps kinds to labels with a fallback', () => {
    expect(kindInfo('compile_error').label).toBe('Build');
    expect(kindInfo('something_new').label).toBe('something new');
  });

  it('formats locations without empty parts', () => {
    expect(locationLabel({ file: 'src/a.ts', line: 3, column: 0, inProject: true })).toBe('src/a.ts:3');
    expect(locationLabel({ file: 'src/a.ts', line: 3, column: 9, inProject: true })).toBe('src/a.ts:3:9');
  });

  it('builds an agent digest preferring project locations', () => {
    const r = toErrorsResult({
      process_id: 'p',
      groups: [
        {
          fingerprint: 'f',
          kind: 'exception',
          message: 'TypeError: x is undefined',
          count: 2,
          first_seen: '2026-11-13T10:00:00Z',
          locations: [
            { file: 'node_modules/lib/a.js', line: 1, in_project: false },
            { file: 'src/app.ts', line: 12, column: 4, in_project: true }
          ],
          sample: ['TypeError: x is undefined'],
          new: true
        }
      ],
      total_errors: 2,
      cursor: '10'
    });
    expect(r.groups[0]!.isNew).toBe(true);
    expect(r.groups[0]!.firstSeen).toBeGreaterThan(0);
    expect(errorsDigest(r.groups)).toBe('1. [exception] TypeError: x is undefined (x2)\n   at src/app.ts:12:4');
  });
});
