import { GuideSection } from '../../api/guides-api';

/**
 * The heading of each section of a format's guide in the reader's language (R-UI-049, Q-UI-86 as amended on 2026-11-27): the heading is
 * the page's frame, the text under it stays the format's English. One unit per section, keyed by the section's stable ID, as
 * `FORMAT_CLIENTS` is the table of the clients' names; the English is the server's `title` word for word. A format's section that is
 * not in this table fails `TestR_UI_049_EveryGuideHeadingHasAUnitInBothLanguages` (internal/formats/all), which reads this file:
 * every entry is `'<id>': $localize\`:@@guide.title.<id>:<the section's title>\``, one per line. A function, not a constant, so that
 * a test that loads the German translations reads them.
 */
function guideTitles(): Readonly<Record<string, string>> {
  return {
    // ansible
    'ansible.local.what': $localize`:@@guide.title.ansible.local.what:What a local repository is`,
    'ansible.remote.what': $localize`:@@guide.title.ansible.remote.what:What a remote repository is`,
    'ansible.virtual.what': $localize`:@@guide.title.ansible.virtual.what:What a virtual repository is`,
    'ansible.configure': $localize`:@@guide.title.ansible.configure:Point ansible-galaxy at the repository`,
    'ansible.authenticate': $localize`:@@guide.title.ansible.authenticate:Which credential ansible-galaxy sends`,
    'ansible.install': $localize`:@@guide.title.ansible.install:Install collections and roles`,
    'ansible.publish': $localize`:@@guide.title.ansible.publish:Publish a collection`,
    'ansible.maintain': $localize`:@@guide.title.ansible.maintain:Remove a version, import tasks and tombstones`,
    'ansible.troubleshooting': $localize`:@@guide.title.ansible.troubleshooting:Troubleshooting`,
    // clamav
    'clamav.what': $localize`:@@guide.title.clamav.what:What this repository is`,
    'clamav.configure': $localize`:@@guide.title.clamav.configure:Point freshclam at Quader`,
    'clamav.authenticate': $localize`:@@guide.title.clamav.authenticate:Sign freshclam in`,
    'clamav.install': $localize`:@@guide.title.clamav.install:Run freshclam`,
    'clamav.troubleshooting': $localize`:@@guide.title.clamav.troubleshooting:Troubleshooting`,
    // deb
    'deb.local.what': $localize`:@@guide.title.deb.local.what:What a local repository is`,
    'deb.remote.what': $localize`:@@guide.title.deb.remote.what:What a remote repository is`,
    'deb.virtual.what': $localize`:@@guide.title.deb.virtual.what:What a virtual repository is`,
    'deb.configure': $localize`:@@guide.title.deb.configure:Add the repository as a source`,
    'deb.remote.keys': $localize`:@@guide.title.deb.remote.keys:Which key the client trusts`,
    'deb.authenticate': $localize`:@@guide.title.deb.authenticate:Which credential APT sends`,
    'deb.install': $localize`:@@guide.title.deb.install:Install a package`,
    'deb.publish': $localize`:@@guide.title.deb.publish:Upload a package`,
    'deb.maintain': $localize`:@@guide.title.deb.maintain:Move, delete and re-sign`,
    'deb.virtual.maintain': $localize`:@@guide.title.deb.virtual.maintain:Members and what a build says about them`,
    'deb.troubleshooting': $localize`:@@guide.title.deb.troubleshooting:Troubleshooting`,
    // generic
    'generic.local.what': $localize`:@@guide.title.generic.local.what:What a generic local repository is`,
    'generic.configure': $localize`:@@guide.title.generic.configure:Point curl at the repository`,
    'generic.wget.configure': $localize`:@@guide.title.generic.wget.configure:Point wget at the repository`,
    'generic.powershell.configure': $localize`:@@guide.title.generic.powershell.configure:Point PowerShell at the repository`,
    'generic.authenticate': $localize`:@@guide.title.generic.authenticate:Sign curl in`,
    'generic.wget.authenticate': $localize`:@@guide.title.generic.wget.authenticate:Sign wget in`,
    'generic.powershell.authenticate': $localize`:@@guide.title.generic.powershell.authenticate:Sign PowerShell in`,
    'generic.install': $localize`:@@guide.title.generic.install:Download files`,
    'generic.wget.install': $localize`:@@guide.title.generic.wget.install:Download files`,
    'generic.powershell.install': $localize`:@@guide.title.generic.powershell.install:Download files`,
    'generic.publish': $localize`:@@guide.title.generic.publish:Upload files`,
    'generic.wget.publish': $localize`:@@guide.title.generic.wget.publish:Upload files`,
    'generic.powershell.publish': $localize`:@@guide.title.generic.powershell.publish:Upload files`,
    'generic.maintain': $localize`:@@guide.title.generic.maintain:Replace and delete files`,
    'generic.wget.maintain': $localize`:@@guide.title.generic.wget.maintain:Replace and delete files`,
    'generic.powershell.maintain': $localize`:@@guide.title.generic.powershell.maintain:Replace and delete files`,
    'generic.endpoint': $localize`:@@guide.title.generic.endpoint:On this endpoint`,
    'generic.troubleshooting': $localize`:@@guide.title.generic.troubleshooting:Troubleshooting`,
    // npm
    'npm.local.what': $localize`:@@guide.title.npm.local.what:What a local repository is`,
    'npm.remote.what': $localize`:@@guide.title.npm.remote.what:What a remote repository is`,
    'npm.virtual.what': $localize`:@@guide.title.npm.virtual.what:What a virtual repository is`,
    'npm.configure': $localize`:@@guide.title.npm.configure:Point the client at the repository`,
    'npm.authenticate': $localize`:@@guide.title.npm.authenticate:Sign the client in`,
    'npm.install': $localize`:@@guide.title.npm.install:Install packages`,
    'npm.publish': $localize`:@@guide.title.npm.publish:Publish a package`,
    'npm.maintain': $localize`:@@guide.title.npm.maintain:Dist-tags and unpublish`,
    'npm.audit': $localize`:@@guide.title.npm.audit:Security audit`,
    'npm.lockfiles': $localize`:@@guide.title.npm.lockfiles:Lockfiles`,
    'npm.endpoint': $localize`:@@guide.title.npm.endpoint:On this endpoint`,
    'npm.troubleshooting': $localize`:@@guide.title.npm.troubleshooting:Troubleshooting`,
    // nuget
    'nuget.local.what': $localize`:@@guide.title.nuget.local.what:What a local repository is`,
    'nuget.remote.what': $localize`:@@guide.title.nuget.remote.what:What a remote repository is`,
    'nuget.virtual.what': $localize`:@@guide.title.nuget.virtual.what:What a virtual repository is`,
    'nuget.configure': $localize`:@@guide.title.nuget.configure:Point the client at the repository`,
    'nuget.authenticate': $localize`:@@guide.title.nuget.authenticate:Sign the client in`,
    'nuget.install': $localize`:@@guide.title.nuget.install:Restore and install packages`,
    'nuget.publish': $localize`:@@guide.title.nuget.publish:Push a package`,
    'nuget.maintain': $localize`:@@guide.title.nuget.maintain:Unlist, relist and delete`,
    'nuget.lockfiles': $localize`:@@guide.title.nuget.lockfiles:Lock files`,
    'nuget.endpoint': $localize`:@@guide.title.nuget.endpoint:On this endpoint`,
    'nuget.troubleshooting': $localize`:@@guide.title.nuget.troubleshooting:Troubleshooting`,
    // oci
    'oci.local.what': $localize`:@@guide.title.oci.local.what:What a local repository is`,
    'oci.remote.what': $localize`:@@guide.title.oci.remote.what:What a remote repository is`,
    'oci.virtual.what': $localize`:@@guide.title.oci.virtual.what:What a virtual repository is`,
    'oci.configure': $localize`:@@guide.title.oci.configure:Sign the client in`,
    'oci.authenticate': $localize`:@@guide.title.oci.authenticate:Which credential the client sends`,
    'oci.install': $localize`:@@guide.title.oci.install:Pull an image`,
    'oci.publish': $localize`:@@guide.title.oci.publish:Push an image`,
    'oci.maintain': $localize`:@@guide.title.oci.maintain:Delete tags, images and layers`,
    'oci.endpoint': $localize`:@@guide.title.oci.endpoint:On this endpoint`,
    'oci.troubleshooting': $localize`:@@guide.title.oci.troubleshooting:Troubleshooting`,
  };
}

/**
 * A section's heading in the reader's language, and whether it is the server's English: a section this table does not know (only a
 * test format's, as the Go test holds every registered one) keeps the server's `title`, marked English by the caller.
 */
export function guideTitle(section: Pick<GuideSection, 'id' | 'title'>): { readonly text: string; readonly english: boolean } {
  const text = guideTitles()[section.id];
  return text === undefined ? { text: section.title, english: true } : { text, english: false };
}
