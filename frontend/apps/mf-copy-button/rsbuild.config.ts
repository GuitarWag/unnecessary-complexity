import { pluginModuleFederation } from '@module-federation/rsbuild-plugin';
import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';

export default defineConfig({
  plugins: [
    pluginReact(),
    pluginModuleFederation({
      name: 'mfCopyButton',
      filename: 'remoteEntry.js',
      exposes: {
        './CopyButton': './src/CopyButton.tsx',
      },
      shared: {
        react: { singleton: true, requiredVersion: '18.3.1' },
        'react-dom': { singleton: true, requiredVersion: '18.3.1' },
      },
    }),
  ],
  server: { port: 5176 },
  dev: { assetPrefix: 'http://localhost:5176' },
  output: { assetPrefix: 'http://localhost:5176' },
});
