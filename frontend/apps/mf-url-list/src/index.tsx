import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { UrlList } from './UrlList.js';

const container = document.getElementById('root');
if (!container) throw new Error('root element missing');
createRoot(container).render(
  <StrictMode>
    <UrlList codes={['demo1', 'demo2']} />
  </StrictMode>,
);
