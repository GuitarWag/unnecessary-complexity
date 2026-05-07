import { pluginModuleFederation } from '@module-federation/rsbuild-plugin';
import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';

const remoteUrl = (port: number) => `http://localhost:${port}/mf-manifest.json`;

export default defineConfig({
  plugins: [
    pluginReact(),
    pluginModuleFederation({
      name: 'shell',
      remotes: {
        mfUrlInput: `mfUrlInput@${remoteUrl(5174)}`,
        mfCreateButton: `mfCreateButton@${remoteUrl(5175)}`,
        mfCopyButton: `mfCopyButton@${remoteUrl(5176)}`,
        mfUrlList: `mfUrlList@${remoteUrl(5177)}`,
        mfAnalyticsChart: `mfAnalyticsChart@${remoteUrl(5178)}`,
        mfLoadTest: `mfLoadTest@${remoteUrl(5179)}`,
      },
      shared: {
        react: { singleton: true, requiredVersion: '18.3.1' },
        'react-dom': { singleton: true, requiredVersion: '18.3.1' },
        'react-router-dom': { singleton: true, requiredVersion: '6.28.0' },
      },
    }),
  ],
  html: {
    title: 'URL Shortener',
    meta: { description: 'Ridiculously over-engineered URL shortener' },
  },
  server: { port: 5173 },
});
