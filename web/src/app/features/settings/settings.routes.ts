import { Routes } from '@angular/router';

/**
 * The group Settings of the admin area (Q-UI-326): General (R-UI-033) first, where `/admin/settings` leads, then Authentication
 * (R-UI-031), then Malware scanning (R-MAL-023, Q-UI-870), whose entry the side bar shows while the scanning is on, Signatures
 * (R-SIG-036) and Storage (R-UI-200). The other P1 pages join the group when they are built.
 */
export const SETTINGS_ROUTES: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'general' },
  {
    path: 'general',
    title: $localize`:@@route.settings.general.title:General settings`,
    loadComponent: () => import('./general-settings').then((m) => m.GeneralSettings),
  },
  {
    path: 'auth',
    title: $localize`:@@route.settings.auth.title:Authentication settings`,
    loadComponent: () => import('./auth-settings').then((m) => m.AuthSettings),
  },
  {
    path: 'malware',
    title: $localize`:@@route.settings.malware.title:Malware scanning`,
    loadComponent: () => import('./malware-settings').then((m) => m.MalwareSettingsPage),
  },
  {
    // The fourth page of the group (R-SIG-036): the trust anchors, the switch for project Owners, blocks and the public root.
    path: 'signatures',
    title: $localize`:@@route.settings.signatures.title:Signatures`,
    loadComponent: () => import('./signature-settings').then((m) => m.SignatureSettingsPage),
  },
  {
    // The fifth page of the group (R-UI-200, Q-UI-950): Storage, built with its section Download links first.
    path: 'storage',
    title: $localize`:@@route.settings.storage.title:Storage settings`,
    loadComponent: () => import('./storage-settings').then((m) => m.StorageSettingsPage),
  },
];
