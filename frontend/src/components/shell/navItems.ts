import { matchPath } from 'react-router-dom';
import type { ComponentType } from 'react';
import {
  AnalyticsIcon,
  AppsIcon,
  AuditIcon,
  CloudIcon,
  ComposeIcon,
  DockerIcon,
  EnvironmentsIcon,
  HostsIcon,
  KubernetesIcon,
  NotificationsIcon,
  PipelinesIcon,
  RegistryIcon,
  SchedulesIcon,
  SettingsIcon,
  TemplatesIcon,
  type IconProps,
} from '../icons';

export type NavGroupId = 'build' | 'infra' | 'operate';

export interface NavGroup {
  id: NavGroupId;
  label: string;
}

export interface NavItem {
  id: string;
  label: string;
  /** Route the rail item navigates to. */
  to: string;
  group: NavGroupId;
  icon: ComponentType<IconProps>;
  /**
   * react-router path patterns that light this item. Prefix patterns end in
   * `/*`. When several items match, the one with the most static segments
   * wins (so `/docker/compose` lights Compose, not Docker).
   */
  matchPatterns: string[];
}

export const NAV_GROUPS: NavGroup[] = [
  { id: 'build', label: 'Build' },
  { id: 'infra', label: 'Infrastructure' },
  { id: 'operate', label: 'Operate' },
];

const prefix = (base: string) => [base, `${base}/*`];

export const NAV_ITEMS: NavItem[] = [
  { id: 'pipelines', label: 'Pipelines', to: '/pipelines', group: 'build', icon: PipelinesIcon, matchPatterns: prefix('/pipelines') },
  { id: 'apps', label: 'Apps', to: '/apps', group: 'build', icon: AppsIcon, matchPatterns: ['/', ...prefix('/apps')] },

  { id: 'docker', label: 'Docker', to: '/docker', group: 'infra', icon: DockerIcon, matchPatterns: prefix('/docker') },
  { id: 'compose', label: 'Compose', to: '/docker/compose', group: 'infra', icon: ComposeIcon, matchPatterns: prefix('/docker/compose') },
  { id: 'kubernetes', label: 'Kubernetes', to: '/kubernetes', group: 'infra', icon: KubernetesIcon, matchPatterns: prefix('/kubernetes') },
  { id: 'cloud', label: 'Cloud', to: '/cloud', group: 'infra', icon: CloudIcon, matchPatterns: prefix('/cloud') },
  { id: 'environments', label: 'Environments', to: '/environments', group: 'infra', icon: EnvironmentsIcon, matchPatterns: prefix('/environments') },
  { id: 'hosts', label: 'Hosts', to: '/hosts', group: 'infra', icon: HostsIcon, matchPatterns: prefix('/hosts') },
  { id: 'registry', label: 'Registry', to: '/registry', group: 'infra', icon: RegistryIcon, matchPatterns: prefix('/registry') },

  { id: 'templates', label: 'Templates', to: '/admin/templates', group: 'operate', icon: TemplatesIcon, matchPatterns: prefix('/admin/templates') },
  { id: 'schedules', label: 'Schedules', to: '/admin/schedules', group: 'operate', icon: SchedulesIcon, matchPatterns: prefix('/admin/schedules') },
  { id: 'notifications', label: 'Notifications', to: '/admin/notifications', group: 'operate', icon: NotificationsIcon, matchPatterns: prefix('/admin/notifications') },
  { id: 'audit', label: 'Audit', to: '/admin/audit', group: 'operate', icon: AuditIcon, matchPatterns: prefix('/admin/audit') },
  { id: 'analytics', label: 'Analytics', to: '/analytics', group: 'operate', icon: AnalyticsIcon, matchPatterns: prefix('/analytics') },
  { id: 'settings', label: 'Settings', to: '/settings', group: 'operate', icon: SettingsIcon, matchPatterns: prefix('/settings') },
];

/** Number of static path segments in a pattern — the specificity score. */
function specificity(pattern: string): number {
  return pattern
    .replace(/\/\*$/, '')
    .split('/')
    .filter(Boolean).length;
}

/**
 * activeNavId — which rail item owns `pathname`, or null when none does.
 * Pure: safe to unit-test without rendering.
 */
export function activeNavId(pathname: string): string | null {
  let best: { id: string; score: number } | null = null;
  for (const item of NAV_ITEMS) {
    for (const pattern of item.matchPatterns) {
      if (!matchPath(pattern, pathname)) continue;
      const score = specificity(pattern);
      if (!best || score > best.score) best = { id: item.id, score };
    }
  }
  return best ? best.id : null;
}

export function navItemById(id: string): NavItem | undefined {
  return NAV_ITEMS.find((i) => i.id === id);
}
