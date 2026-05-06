import { pluginModuleFederation } from '@module-federation/rsbuild-plugin';
import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';

export default defineConfig({
  plugins: [
    pluginReact(),
    pluginModuleFederation({
      name: 'mfUrlList',
      filename: 'remoteEntry.js',
      exposes: {
        './UrlList': './src/UrlList.tsx',
      },
      shared: {
        react: { singleton: true, requiredVersion: '18.3.1' },
        'react-dom': { singleton: true, requiredVersion: '18.3.1' },
      },
    }),
  ],
  server: { port: 5177 },
  dev: { assetPrefix: 'http://localhost:5177' },
  output: { assetPrefix: 'http://localhost:5177' },
});
