import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { UrlInput } from './UrlInput.js';

const container = document.getElementById('root');
if (!container) throw new Error('root element missing');
createRoot(container).render(
  <StrictMode>
    <UrlInput />
  </StrictMode>,
);
