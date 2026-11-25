import { CLAMAV_DEFINITIONS_FORMAT, GENERIC_FORMAT, NUGET_FORMAT } from '../../api/testing/fakes-catalog';
import {
  classFilterQuery,
  classFilterValues,
  classHelp,
  classIcon,
  classLabel,
  classTip,
  creatableFormats,
  shownClass,
} from './repository-class';

/** The catalog as the server lists it: two formats a person creates repositories of, and one that is system only. */
const CATALOG = [GENERIC_FORMAT, CLAMAV_DEFINITIONS_FORMAT, NUGET_FORMAT];
/** A future system-only format, so that the rule is the catalog's fact and no format's name. */
const OTHER_SYSTEM = { ...GENERIC_FORMAT, name: 'signatures-db', systemOnly: true };

describe('The one place that names a repository\'s class (R-UI-020)', () => {
  it('R-UI-020 R-CLAMAV-001 a repository of a system-only format is shown as System, every other as its class, by the catalog\'s fact', () => {
    expect(shownClass({ class: 'local', format: 'clamav' }, CATALOG)).toBe('system');
    expect(shownClass({ class: 'local', format: 'signatures-db' }, [...CATALOG, OTHER_SYSTEM])).toBe('system');
    expect(shownClass({ class: 'local', format: 'generic' }, CATALOG)).toBe('local');
    expect(shownClass({ class: 'remote', format: 'nuget' }, CATALOG)).toBe('remote');
    // While the catalog is not known the class stands as it is.
    expect(shownClass({ class: 'local', format: 'clamav' }, undefined)).toBe('local');
  });

  it('R-UI-020 System has its word, the icon of the usage pages\' caller System, and its sentence as the tooltip', () => {
    expect(classLabel('system')).toBe('System');
    expect(classIcon('system')).toBe('settings');
    expect(classHelp('system')).toBe('Filled by this installation itself, on a schedule; nobody publishes to it.');
    expect(classTip('system')).toBe(classHelp('system'));
    expect(['local', 'remote', 'virtual'].map(classTip)).toEqual(['', '', '']);
    expect(['local', 'remote', 'virtual'].map(classIcon)).toEqual(['storage', 'cloud_download', 'layers']);
  });

  it('R-UI-080 the formats a person creates repositories of leave every system-only one out, in the catalog\'s order', () => {
    expect(creatableFormats([...CATALOG, OTHER_SYSTEM]).map((f) => f.name)).toEqual(['generic', 'nuget']);
  });

  it('R-UI-020 a filter by class offers System where the catalog has a system-only format, and asks the server by class and format', () => {
    expect(classFilterValues(CATALOG)).toEqual(['local', 'remote', 'virtual', 'system']);
    expect(classFilterValues([GENERIC_FORMAT])).toEqual(['local', 'remote', 'virtual']);
    // Local is the class local of every format that is not system only, System of those that are.
    expect(classFilterQuery('local', undefined, CATALOG)).toEqual({ class: 'local', format: 'generic,nuget' });
    expect(classFilterQuery('system', undefined, CATALOG)).toEqual({ class: 'local', format: 'clamav' });
    expect(classFilterQuery('local', 'nuget', CATALOG)).toEqual({ class: 'local', format: 'nuget' });
    // A format and a class that exclude each other match nothing, and nothing is asked.
    expect(classFilterQuery('system', 'nuget', CATALOG)).toBeNull();
    expect(classFilterQuery('local', 'clamav', CATALOG)).toBeNull();
    // Without a system-only format, and for the other classes, the parameters stand as they are.
    expect(classFilterQuery('local', undefined, [GENERIC_FORMAT])).toEqual({ class: 'local', format: undefined });
    expect(classFilterQuery('remote', 'nuget', CATALOG)).toEqual({ class: 'remote', format: 'nuget' });
    expect(classFilterQuery(undefined, undefined, CATALOG)).toEqual({ class: undefined, format: undefined });
  });
});
