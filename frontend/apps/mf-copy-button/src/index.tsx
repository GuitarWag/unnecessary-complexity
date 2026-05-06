import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { CopyButton } from './CopyButton.js';

const container = document.getElementById('root');
if (!container) throw new Error('root element missing');
createRoot(container).render(
  <StrictMode>
    <CopyButton text="hello world" />
  </StrictMode>,
);
