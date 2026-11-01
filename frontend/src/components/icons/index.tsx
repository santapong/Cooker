import type { SVGProps } from 'react';

/**
 * Inline line-icon set — 18×18 grid, 1.5px stroke, `currentColor`, no fills.
 * Hand-drawn so the frontend adds no icon dependency. Every icon is
 * `aria-hidden`; the owning control must carry the accessible name.
 */
export type IconProps = SVGProps<SVGSVGElement>;

function Icon({ children, ...rest }: IconProps) {
  return (
    <svg
      viewBox="0 0 18 18"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...rest}
    >
      {children}
    </svg>
  );
}

/* ── rail: build ── */
export const PipelinesIcon = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="4" cy="9" r="2" />
    <circle cx="14" cy="4" r="2" />
    <circle cx="14" cy="14" r="2" />
    <path d="M6 8l6-3M6 10l6 3" />
  </Icon>
);
export const AppsIcon = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="3" width="5" height="5" rx="1" />
    <rect x="10" y="3" width="5" height="5" rx="1" />
    <rect x="3" y="10" width="5" height="5" rx="1" />
    <rect x="10" y="10" width="5" height="5" rx="1" />
  </Icon>
);

/* ── rail: infrastructure ── */
export const DockerIcon = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="6.5" width="12" height="8.5" rx="1.2" />
    <path d="M3 10h12M7 6.5V4h4v2.5" />
  </Icon>
);
export const ComposeIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M9 3l6 3-6 3-6-3z" />
    <path d="M3 9l6 3 6-3M3 12l6 3 6-3" />
  </Icon>
);
export const KubernetesIcon = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="9" cy="9" r="6" />
    <circle cx="9" cy="9" r="1.5" />
    <path d="M9 3v4.5M9 10.5V15M3.8 6l3.9 2.25M10.3 9.75L14.2 12M3.8 12l3.9-2.25M10.3 8.25L14.2 6" />
  </Icon>
);
export const CloudIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M5.5 14h7.5a3 3 0 0 0 .4-5.97A4.5 4.5 0 0 0 4.8 8.6 2.75 2.75 0 0 0 5.5 14z" />
  </Icon>
);
export const EnvironmentsIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 5h12M3 9h12M3 13h12" />
    <circle cx="6.5" cy="5" r="1.4" fill="var(--hull-0)" />
    <circle cx="11" cy="9" r="1.4" fill="var(--hull-0)" />
    <circle cx="8" cy="13" r="1.4" fill="var(--hull-0)" />
  </Icon>
);
export const HostsIcon = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="3" width="12" height="5" rx="1" />
    <rect x="3" y="10" width="12" height="5" rx="1" />
    <path d="M6 5.5h.01M6 12.5h.01" />
  </Icon>
);
export const RegistryIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 6l6-3 6 3v7l-6 3-6-3z" />
    <path d="M3 6l6 3 6-3M9 9v7" />
  </Icon>
);

/* ── rail: operate ── */
export const TemplatesIcon = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="3" width="12" height="12" rx="1.5" />
    <path d="M3 7h12M7.5 7v8" />
  </Icon>
);
export const SchedulesIcon = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="9" cy="9" r="6" />
    <path d="M9 5.5V9l2.5 1.5" />
  </Icon>
);
export const NotificationsIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M5 12V8a4 4 0 0 1 8 0v4l1.5 1.5h-11z" />
    <path d="M7.5 15.5a1.5 1.5 0 0 0 3 0" />
  </Icon>
);
export const AuditIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M4 4h10M4 8.5h10M4 13h5" />
    <path d="M11.5 13l1.5 1.5L16 11.5" />
  </Icon>
);
export const AnalyticsIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 15h12M5.5 12.5V8M9 12.5V4M12.5 12.5V9.5" />
  </Icon>
);
export const SettingsIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 5h7.5M14.5 5H15M3 9h2M8.5 9H15M3 13h6M13 13h2" />
    <circle cx="12.5" cy="5" r="1.5" />
    <circle cx="6.5" cy="9" r="1.5" />
    <circle cx="11" cy="13" r="1.5" />
  </Icon>
);

/* ── strip controls ── */
export const CalmIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M14 11.2A6 6 0 0 1 6.8 4a6 6 0 1 0 7.2 7.2z" />
  </Icon>
);
export const LogoutIcon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M7.5 3H4v12h3.5M11.5 12.5L15 9l-3.5-3.5M15 9H7" />
  </Icon>
);
