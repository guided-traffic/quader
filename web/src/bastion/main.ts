import { bootstrapApplication } from '@angular/platform-browser';
import { bastionConfig } from './app.config';
import { BastionApp } from './app';

bootstrapApplication(BastionApp, bastionConfig).catch((err: unknown) => {
  console.error(err);
});
