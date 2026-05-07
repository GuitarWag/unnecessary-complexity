import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { LoadTest } from './LoadTest.js';

const container = document.getElementById('root');
if (!container) throw new Error('root element missing');
createRoot(container).render(
  <StrictMode>
    <LoadTest />
  </StrictMode>,
);
