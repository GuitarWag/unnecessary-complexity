import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { CreateButton } from './CreateButton.js';

const container = document.getElementById('root');
if (!container) throw new Error('root element missing');
createRoot(container).render(
  <StrictMode>
    <CreateButton onClick={() => alert('clicked')}>Click me</CreateButton>
  </StrictMode>,
);
