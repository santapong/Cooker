/** Read-only Vercel installation metadata; these records are not services or resource bindings. */
export interface IntegrationInstallation {
  id: string;
  integrationId: string;
  slug?: string;
  name: string;
  /** Known values: marketplace, external, unknown. Future values remain displayable as unknown. */
  installationType: string;
  /** A provider-reported installation state, never a resource health result. */
  providerStatus: string;
  /** Known values: all_projects, selected_projects. */
  projectAccess: string;
  /** Only project IDs in the current selected scope are included by the server. */
  projectIds: string[];
}

export interface IntegrationInventory {
  items: IntegrationInstallation[];
  /** Latest successful inventory time; a failed refresh does not advance this clock. */
  observedAt?: string;
  attemptedAt?: string;
  /** Known values: available, stale, unavailable. A missing inventory is not observed. */
  state: string;
  errorCode?: string;
  /** Cooker-authored explanation; never a raw provider response. */
  error?: string;
  /** Coverage of the retained successful observation, including when stale. */
  complete: boolean;
}
