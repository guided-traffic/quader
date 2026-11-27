import { inject } from '@angular/core';
import { Router, Routes } from '@angular/router';
import { capabilityGuard } from '../../core/auth-guard';

/** R-BAS-007; a Bastion's limits page (R-UI-241) for the holders of `limits:read`, its projects page (Q-UI-1637) for those of `endpoints:read`. */
export const BASTIONS_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@route.bastions.title:Bastions`,
    loadComponent: () => import('./bastion-list').then((m) => m.BastionList),
  },
  {
    // A registration has no page of its own: the trail's step of its name leads to its card on the list.
    path: ':name',
    pathMatch: 'full',
    redirectTo: ({ params }) => inject(Router).createUrlTree(['/admin/bastions'], { fragment: `bastion-${params['name'] ?? ''}` }),
  },
  {
    // Every project a Bastion allows (R-BAS-134, Q-UI-1637): the card's section Projects shows ten and leads here.
    path: ':name/projects',
    title: $localize`:@@bastion.projects.heading:Projects`,
    canActivate: [capabilityGuard('endpoints:read')],
    loadComponent: () => import('./bastion-projects').then((m) => m.BastionProjectsPage),
  },
  {
    path: ':name/limits',
    title: $localize`:@@limits.heading:Download limits`,
    canActivate: [capabilityGuard('limits:read')],
    loadComponent: () => import('./bastion-limits-page').then((m) => m.BastionLimitsPage),
    children: [
      {
        path: '',
        pathMatch: 'full',
        title: $localize`:@@limits.heading:Download limits`,
        loadComponent: () => import('./bastion-limits-page').then((m) => m.BastionRulesTab),
      },
      {
        path: 'blocks',
        title: $localize`:@@limits.title.blocks:Blocks of the download limits`,
        loadComponent: () => import('./bastion-limits-page').then((m) => m.BastionBlocksTab),
      },
    ],
  },
];
