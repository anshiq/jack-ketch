import { describe, expect, it } from 'vitest';
import { duplicateOwner } from './startGuard';

describe('duplicateOwner', () => {
  it('extracts the owning project from the daemon error', () => {
    const err = new Error('failed_precondition: ./mvnw is already running in project buildercrm (/home/me/buildercrm) with the same command and workdir; stop it first');
    expect(duplicateOwner(err)).toBe('project buildercrm (/home/me/buildercrm)');
  });

  it('ignores unrelated errors', () => {
    expect(duplicateOwner(new Error('port 8080 in use'))).toBeNull();
    expect(duplicateOwner('boom')).toBeNull();
  });
});
