import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { AnalyticsChart } from './AnalyticsChart.js';

const container = document.getElementById('root');
if (!container) throw new Error('root element missing');
createRoot(container).render(
  <StrictMode>
    <AnalyticsChart code="demo" />
  </StrictMode>,
);
