import { pluginModuleFederation } from '@module-federation/rsbuild-plugin';
import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';

export default defineConfig({
  plugins: [
    pluginReact(),
    pluginModuleFederation({
      name: 'mfUrlInput',
      filename: 'remoteEntry.js',
      exposes: {
        './UrlInput': './src/UrlInput.tsx',
      },
      shared: {
        react: { singleton: true, requiredVersion: '18.3.1' },
        'react-dom': { singleton: true, requiredVersion: '18.3.1' },
      },
    }),
  ],
  server: { port: 5174 },
  dev: { assetPrefix: 'http://localhost:5174' },
  output: { assetPrefix: 'http://localhost:5174' },
});
