import type { SVGProps } from 'react';
import type { StageKind } from './stageKinds';

/** Type has a stable silhouette; colour remains available for execution status. */
export default function StageSymbol({ kind, ...props }: SVGProps<SVGSVGElement> & { kind: StageKind }) {
  let shape;
  switch (kind) {
    case 'build':
      shape = <><path d="m9 1.5 6.5 3.7v7.6L9 16.5l-6.5-3.7V5.2z" /><path d="m2.5 5.2 6.5 4 6.5-4M9 9.2v7.3M5.8 3.4l6.5 3.8" /></>;
      break;
    case 'test':
      shape = <><path d="M6 2h6M7 2v5l-4.5 6.5A1.5 1.5 0 0 0 3.8 16h10.4a1.5 1.5 0 0 0 1.3-2.5L11 7V2M5 10h8" /><path d="m7 13 1.5 1.5L11 12" /></>;
      break;
    case 'push':
      shape = <><path d="M9 12V2m-4 4 4-4 4 4M3 11v5h12v-5" /><path d="M6 16h6" /></>;
      break;
    case 'deploy':
      shape = <><path d="M6 12c0-5 4-9 10-10 0 6-4 10-9 10zM6 7H3l-1 5h4m5 0v3l-5 1v-4M4 14l-2 2" /><circle cx="12" cy="6" r="1.5" /></>;
      break;
    case 'approval':
      shape = <><path d="m9 1 8 8-8 8-8-8z" /><path d="m5.5 9 2.3 2.3 4.7-4.6" /></>;
      break;
    case 'gitops-commit':
      shape = <><circle cx="5" cy="3" r="2" /><circle cx="5" cy="15" r="2" /><circle cx="13" cy="5" r="2" /><path d="M5 5v8m0-3h4a4 4 0 0 0 4-3" /></>;
      break;
    case 'service':
      shape = <><rect x="2" y="3" width="14" height="12" rx="2" /><path d="M2 7h14M5 10v2m4-2v2m4-2v2" /></>;
      break;
    case 'external':
      shape = <><ellipse cx="9" cy="4" rx="6" ry="2.5" /><path d="M3 4v10c0 3.3 12 3.3 12 0V4M3 9c0 3.3 12 3.3 12 0" /></>;
      break;
    default:
      shape = <><rect x="1.5" y="2.5" width="15" height="13" rx="2" /><path d="m5 6 3 3-3 3m5 0h3" /></>;
  }
  return (
    <svg viewBox="0 0 18 18" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" {...props}>
      {shape}
    </svg>
  );
}
