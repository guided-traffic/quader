import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, RouterOutlet, provideRouter } from '@angular/router';
import { settle } from '../testing/screen';
import { FocusOnNavigation } from './focus-on-navigation';

@Component({ template: `<h1 tabindex="-1">Repositories</h1><input aria-label="Search" />` })
class ListScreen {}

@Component({ template: `<h1 tabindex="-1">Settings</h1>` })
class OtherScreen {}

@Component({ imports: [RouterOutlet], template: `<main id="main" tabindex="-1"><router-outlet /></main>` })
class Shell {}

describe('FocusOnNavigation (R-UI-044)', () => {
  it("R-UI-044 moves the focus to another screen's h1, and leaves it in a list's field when a navigation changes only the query", async () => {
    TestBed.configureTestingModule({
      providers: [provideRouter([{ path: 'list', component: ListScreen }, { path: 'other', component: OtherScreen }])],
    });
    const fixture = TestBed.createComponent(Shell);
    const root = fixture.nativeElement as HTMLElement;
    TestBed.inject(FocusOnNavigation).start();
    const router = TestBed.inject(Router);
    const go = async (url: string): Promise<void> => {
      await router.navigateByUrl(url);
      await settle(fixture);
    };

    // A fresh load leaves the focus alone (the skip link comes first).
    await go('/list');
    expect(document.activeElement).toBe(document.body);

    await go('/other');
    expect(document.activeElement).toBe(root.querySelector('h1'));

    await go('/list');
    const field = root.querySelector('input')!;
    field.focus();
    // The filter writes what is typed into the address, as every list does 250 ms after the last key.
    await go('/list?q=np');
    await go('/list?q=npm#top');
    expect(document.activeElement).toBe(field);

    await go('/other?q=npm');
    expect(document.activeElement).toBe(root.querySelector('h1'));
  });
});
